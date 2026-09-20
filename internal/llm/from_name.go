package llm

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

// defaultGeminiModel is what runs when GEMINI_MODEL is unset.
// It is a measured choice, not a guess: the 2.5 family now answers 404 for
// new keys, 3.6 through 3.8 answered 503, and this one and
// gemini-3.1-flash-lite were the two that actually responded. A default
// pointing at a retired model fails with a 404 that says nothing about the
// real cause.
const defaultGeminiModel = "gemini-3.5-flash"

// responseHeaderTimeout bounds the wait for the provider's FIRST byte.
// Measured on this project's free Gemini tier: one request in three took 68
// seconds to first byte while the others took under 10. A ceiling anywhere
// near the median turns ordinary tail latency into a failed run.
const responseHeaderTimeout = 180 * time.Second

// transportClient builds the transport used by the hand-written adapters.
//
// It deliberately leaves http.Client.Timeout unset. That field bounds the
// whole exchange INCLUDING reading the body, so on a streaming response it
// cuts off an answer that is arriving correctly - the longer the useful
// output, the likelier it is killed. What actually needs a ceiling is the
// wait for a provider that never answers, and that is a different clock:
// ResponseHeaderTimeout. Cancellation of a healthy-but-long stream stays with
// the context the caller already passes.
//
// The transport is cloned from the default so proxy handling, dialing and
// HTTP/2 keep working; building an http.Transport from scratch silently drops
// all three.
func transportClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = responseHeaderTimeout
	return &http.Client{Transport: tr}
}

// FromName builds one provider by its config name, reading its credentials
// from the environment. It is the single place each provider is wired, retry
// policy included - Claude is not wrapped in WithRetry because its official
// SDK classifies and retries on its own, and two stacked policies multiply
// the wait without improving the odds.
//
// A named provider whose credential is missing is an ERROR that names the
// variable, never a fallback: the name came from the user's config, and the
// measured symptom of building something else instead is "the agent cannot
// use tools", a session away from its cause.
func FromName(name string) (Client, error) {
	switch name {
	case "claude":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("provider %q needs ANTHROPIC_API_KEY, which is unset", name)
		}
		return NewClaudeClient(key, os.Getenv("ANTHROPIC_MODEL")), nil
	case "gemini":
		key := os.Getenv("GEMINI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("provider %q needs GEMINI_API_KEY, which is unset", name)
		}
		model := os.Getenv("GEMINI_MODEL")
		if model == "" {
			model = defaultGeminiModel
		}
		return WithRetry(&GeminiClient{APIKey: key, Model: model, HTTP: transportClient()}, DefaultAttempts), nil
	case "openai":
		baseURL := os.Getenv("OPENAI_BASE_URL")
		if baseURL == "" {
			baseURL = "http://localhost:11434/v1"
		}
		model := os.Getenv("OPENAI_MODEL")
		if model == "" {
			model = "llama3"
		}
		return WithRetry(&OpenAIClient{
			BaseURL: baseURL,
			APIKey:  os.Getenv("OPENAI_API_KEY"),
			Model:   model,
			HTTP:    transportClient(),
		}, DefaultAttempts), nil
	default:
		return nil, fmt.Errorf("unknown provider %q (valid: claude, gemini, openai)", name)
	}
}
