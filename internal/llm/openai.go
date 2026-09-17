package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// OpenAIClient implements the Client interface for OpenAI-compatible APIs (like Ollama, LM Studio, etc.)
type OpenAIClient struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
	Stream   bool            `json:"stream,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	// Usage is optional in the wire format: llama.cpp and some local servers
	// omit it entirely, which is why the fallback below exists.
	Usage struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

func (c *OpenAIClient) Generate(ctx context.Context, messages []Message) (Response, error) {
	reqBody := openAIRequest{
		Model: c.Model,
	}

	for _, m := range messages {
		reqBody.Messages = append(reqBody.Messages, openAIMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return Response{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return Response{}, err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return Response{}, NewAPIError("openai", resp.StatusCode, body, resp.Header)
	}

	var aiResp openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&aiResp); err != nil {
		return Response{}, err
	}

	if len(aiResp.Choices) == 0 {
		return Response{}, errors.New("empty response from API")
	}

	text := aiResp.Choices[0].Message.Content

	u := Usage{
		InputTokens:     aiResp.Usage.PromptTokens,
		OutputTokens:    aiResp.Usage.CompletionTokens,
		CacheReadTokens: aiResp.Usage.PromptTokensDetails.CachedTokens,
	}
	if u.InputTokens == 0 && u.OutputTokens == 0 {
		// The server reported nothing. An estimate that says it is an estimate
		// beats a zero that looks like a free call.
		u = Usage{
			InputTokens:  EstimateMessages(messages),
			OutputTokens: EstimateTokens(text),
			Estimated:    true,
		}
	}

	return Response{Text: text, Usage: u}, nil
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	// Usage is only sent in the last chunk if stream_options.include_usage is set,
	// but for simplicity we can estimate if omitted.
	Usage *struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

func (c *OpenAIClient) GenerateStream(ctx context.Context, messages []Message, onChunk func(string)) (Response, error) {
	reqBody := openAIRequest{
		Model:  c.Model,
		Stream: true,
	}

	for _, m := range messages {
		reqBody.Messages = append(reqBody.Messages, openAIMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return Response{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return Response{}, err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return Response{}, NewAPIError("openai", resp.StatusCode, body, resp.Header)
	}

	var fullText strings.Builder
	var lastUsage Usage

	err = readSSE(resp.Body, func(data []byte) error {
		var chunk openAIStreamChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return err
		}
		if len(chunk.Choices) > 0 {
			text := chunk.Choices[0].Delta.Content
			fullText.WriteString(text)
			if text != "" {
				onChunk(text)
			}
		}
		if chunk.Usage != nil {
			lastUsage = Usage{
				InputTokens:     chunk.Usage.PromptTokens,
				OutputTokens:    chunk.Usage.CompletionTokens,
				CacheReadTokens: chunk.Usage.PromptTokensDetails.CachedTokens,
			}
		}
		return nil
	})

	if lastUsage.InputTokens == 0 && lastUsage.OutputTokens == 0 {
		lastUsage = Usage{
			InputTokens:  EstimateMessages(messages),
			OutputTokens: EstimateTokens(fullText.String()),
			Estimated:    true,
		}
	}

	return Response{Text: fullText.String(), Usage: lastUsage}, err
}
