package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultClaudeModel is the model used unless the caller names another.
const DefaultClaudeModel = "claude-opus-5"

// ClaudeClient talks to the Claude API through the official SDK.
type ClaudeClient struct {
	APIKey string
	Model  string
	// MaxTokens caps the response. 16000 is the non-streaming default:
	// lowballing it truncates mid-thought and costs a retry.
	MaxTokens int64

	client *anthropic.Client
}

func NewClaudeClient(apiKey, model string) *ClaudeClient {
	if model == "" {
		model = DefaultClaudeModel
	}
	var opts []option.RequestOption
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	c := anthropic.NewClient(opts...)
	return &ClaudeClient{APIKey: apiKey, Model: model, MaxTokens: 16000, client: &c}
}

func (c *ClaudeClient) Describe() Capabilities {
	return Capabilities{
		Provider:         "anthropic",
		Model:            c.Model,
		Vision:           Supported,
		Thinking:         Supported,
		PromptCaching:    Supported,
		MaxContextTokens: 1000000,
	}
}

func (c *ClaudeClient) Generate(ctx context.Context, messages []Message) (Response, error) {
	if c.client == nil {
		return Response{}, fmt.Errorf("claude client not initialised; use NewClaudeClient")
	}

	// The Messages API takes the system prompt as its own field, not as a
	// message with role "system".
	var system []anthropic.TextBlockParam
	var turns []anthropic.MessageParam

	for _, m := range messages {
		switch m.Role {
		case "system":
			system = append(system, anthropic.TextBlockParam{Text: m.Content})
		case "assistant":
			turns = append(turns, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content)))
		default:
			turns = append(turns, anthropic.NewUserMessage(blocksFor(m)...))
		}
	}

	// Mark the end of the stable prefix so it is billed at a discount on the
	// next call. The system prompt here is phase instructions plus the tool
	// protocol plus the MCP catalog: identical across every round of the tool
	// loop, which is exactly what a prefix cache is for.
	//
	// The design that makes this pay is provider-agnostic - keep the stable
	// content first and never interpolate anything volatile into it. Claude
	// needs the explicit marker, OpenAI-compatible endpoints do it on their
	// own, and a local server may do nothing; in all three cases a stable
	// prefix is the right shape.
	if n := len(system); n > 0 {
		system[n-1].CacheControl = anthropic.NewCacheControlEphemeralParam()
	}

	// Adaptive thinking: the model decides when and how much to reason.
	adaptive := anthropic.ThinkingConfigAdaptiveParam{}

	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.Model),
		MaxTokens: c.MaxTokens,
		System:    system,
		Messages:  turns,
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive},
	})
	if err != nil {
		return Response{}, err
	}

	// A refusal arrives as a successful response, so stop_reason must be read
	// before the content: treating it as ordinary output would hand the
	// workflow an empty answer that looks like a real one.
	if resp.StopReason == anthropic.StopReasonRefusal {
		return Response{}, fmt.Errorf("the model declined this request (category %q): %s",
			resp.StopDetails.Category, resp.StopDetails.Explanation)
	}

	var out strings.Builder
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			out.WriteString(tb.Text)
		}
	}
	return Response{
		Text: out.String(),
		Usage: Usage{
			InputTokens:      int(resp.Usage.InputTokens),
			OutputTokens:     int(resp.Usage.OutputTokens),
			CacheReadTokens:  int(resp.Usage.CacheReadInputTokens),
			CacheWriteTokens: int(resp.Usage.CacheCreationInputTokens),
		},
	}, nil
}

// blocksFor turns a message into content blocks, images first: a question
// about an image reads better after the image, as with a document.
func blocksFor(m Message) []anthropic.ContentBlockParamUnion {
	var blocks []anthropic.ContentBlockParamUnion
	for _, img := range m.Images {
		blocks = append(blocks, anthropic.NewImageBlockBase64(img.MediaType, encode(img.Data)))
	}
	if m.Content != "" {
		blocks = append(blocks, anthropic.NewTextBlock(m.Content))
	}
	return blocks
}
