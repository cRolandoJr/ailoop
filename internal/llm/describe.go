package llm

import "strings"

// Describe for the Gemini adapter. Vision is a documented property of the
// Gemini family; context length is left at 0 because we have not measured it
// for whatever model string the user configured.
func (c *GeminiClient) Describe() Capabilities {
	return Capabilities{
		Provider: "google",
		Model:    c.Model,
		Vision:   Supported,
		Thinking: Unsupported,
		// Gemini caches explicitly, through an API this adapter does not use.
		PromptCaching: Unsupported,
	}
}

// Describe for any OpenAI-compatible endpoint, which includes Ollama and
// every local server.
//
// Everything here is Unknown on purpose. The same wire protocol fronts models
// that can see images and models that cannot, and the endpoint does not tell
// us which one it is. Guessing "probably yes" would offer an agent a
// capability that silently fails at the first image; guessing "no" would hide
// one that works. AI_LOOP 18.16 gives Unknown as a real answer, and the
// workflow treats it as fail-closed.
func (c *OpenAIClient) Describe() Capabilities {
	provider := "openai-compatible"
	if strings.Contains(c.BaseURL, "localhost") || strings.Contains(c.BaseURL, "127.0.0.1") {
		provider = "local"
	}
	return Capabilities{
		Provider: provider,
		Model:    c.Model,
		Vision:   Unknown,
		Thinking: Unknown,
		// OpenAI caches stable prefixes automatically and reports it as
		// cached_tokens; a local server usually does not. Either way the
		// design answer is the same - keep the prefix stable - so this stays
		// Unknown rather than claiming a discount we cannot confirm.
		PromptCaching: Unknown,
	}
}
