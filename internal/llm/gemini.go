package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// GeminiClient implements the Client interface for the Google Gemini API.
type GeminiClient struct {
	APIKey string
	Model  string
	HTTP   *http.Client
}

type geminiRequest struct {
	// SystemInstruction is where Gemini takes the system prompt. Folding it
	// into the conversation as a user turn (the previous behaviour) changed
	// its meaning: instructions became something the model could argue with
	// instead of something it operates under, and it broke the alternation
	// the API expects.
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

// geminiPart is a union: exactly one field is set.
type geminiPart struct {
	Text       string            `json:"text,omitempty"`
	InlineData *geminiInlineData `json:"inline_data,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount        int `json:"promptTokenCount"`
		CandidatesTokenCount    int `json:"candidatesTokenCount"`
		CachedContentTokenCount int `json:"cachedContentTokenCount"`
	} `json:"usageMetadata"`
}

func (c *GeminiClient) Generate(ctx context.Context, messages []Message) (Response, error) {
	reqBody := buildGeminiRequest(messages)

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return Response{}, err
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", c.Model)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	// The key goes in a header, not the query string: a URL ends up in proxy
	// logs and shell history, a header does not.
	req.Header.Set("x-goog-api-key", c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return Response{}, NewAPIError("gemini", resp.StatusCode, body, resp.Header)
	}

	var aiResp geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&aiResp); err != nil {
		return Response{}, err
	}

	if len(aiResp.Candidates) == 0 || len(aiResp.Candidates[0].Content.Parts) == 0 {
		return Response{}, errors.New("empty response from Gemini API")
	}

	// A candidate can come back as several parts; taking only the first
	// silently truncated longer answers.
	var text strings.Builder
	for _, p := range aiResp.Candidates[0].Content.Parts {
		text.WriteString(p.Text)
	}

	u := Usage{
		InputTokens:     aiResp.UsageMetadata.PromptTokenCount,
		OutputTokens:    aiResp.UsageMetadata.CandidatesTokenCount,
		CacheReadTokens: aiResp.UsageMetadata.CachedContentTokenCount,
	}
	if u.InputTokens == 0 && u.OutputTokens == 0 {
		u = Usage{
			InputTokens:  EstimateMessages(messages),
			OutputTokens: EstimateTokens(text.String()),
			Estimated:    true,
		}
	}

	return Response{Text: text.String(), Usage: u}, nil
}

func (c *GeminiClient) GenerateStream(ctx context.Context, messages []Message, onChunk func(string)) (Response, error) {
	reqBody := buildGeminiRequest(messages)

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return Response{}, err
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?alt=sse", c.Model)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return Response{}, NewAPIError("gemini", resp.StatusCode, body, resp.Header)
	}

	var fullText strings.Builder
	var lastUsage Usage

	err = readSSE(resp.Body, func(data []byte) error {
		var chunk geminiResponse
		if err := json.Unmarshal(data, &chunk); err != nil {
			return err
		}
		if len(chunk.Candidates) > 0 && len(chunk.Candidates[0].Content.Parts) > 0 {
			for _, p := range chunk.Candidates[0].Content.Parts {
				text := p.Text
				fullText.WriteString(text)
				if text != "" {
					onChunk(text)
				}
			}
		}
		if chunk.UsageMetadata.CandidatesTokenCount > 0 || chunk.UsageMetadata.PromptTokenCount > 0 {
			lastUsage = Usage{
				InputTokens:     chunk.UsageMetadata.PromptTokenCount,
				OutputTokens:    chunk.UsageMetadata.CandidatesTokenCount,
				CacheReadTokens: chunk.UsageMetadata.CachedContentTokenCount,
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

// buildGeminiRequest maps the provider-neutral messages onto Gemini's shape.
// Split out so it can be tested without a network call.
func buildGeminiRequest(messages []Message) geminiRequest {
	var req geminiRequest
	var systemParts []geminiPart

	for _, m := range messages {
		if m.Role == "system" {
			if m.Content != "" {
				systemParts = append(systemParts, geminiPart{Text: m.Content})
			}
			continue
		}

		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}

		var parts []geminiPart
		for _, img := range m.Images {
			parts = append(parts, geminiPart{InlineData: &geminiInlineData{
				MimeType: img.MediaType,
				Data:     encode(img.Data),
			}})
		}
		if m.Content != "" {
			parts = append(parts, geminiPart{Text: m.Content})
		}
		if len(parts) == 0 {
			continue
		}

		// Gemini expects alternating roles. Consecutive turns with the same
		// role are merged rather than sent as-is, which the API rejects.
		if n := len(req.Contents); n > 0 && req.Contents[n-1].Role == role {
			req.Contents[n-1].Parts = append(req.Contents[n-1].Parts, parts...)
			continue
		}
		req.Contents = append(req.Contents, geminiContent{Role: role, Parts: parts})
	}

	if len(systemParts) > 0 {
		req.SystemInstruction = &geminiContent{Parts: systemParts}
	}
	return req
}
