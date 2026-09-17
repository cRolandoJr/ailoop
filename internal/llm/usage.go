package llm

import "strings"

// Usage is what one call to a provider cost.
//
// Every provider reports this differently and some report nothing at all, so
// the workflow needs one shape it can add up. When a provider stays silent we
// estimate and say so: a number that might be wrong is useful, a number that
// might be wrong and looks authoritative is not.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	// CacheReadTokens were served from a prompt cache. They are the cheap
	// ones, and watching this number is how you find out whether a cache is
	// actually working.
	CacheReadTokens int `json:"cache_read_tokens,omitempty"`
	// CacheWriteTokens were written into a cache on this call.
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	// Estimated marks numbers the provider did not give us.
	Estimated bool `json:"estimated,omitempty"`
}

func (u Usage) Total() int { return u.InputTokens + u.OutputTokens }

// Add accumulates another call's usage. Estimated is sticky: a total that
// mixes measured and estimated numbers is an estimate.
func (u *Usage) Add(other Usage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.CacheReadTokens += other.CacheReadTokens
	u.CacheWriteTokens += other.CacheWriteTokens
	if other.Estimated {
		u.Estimated = true
	}
}

// CacheHitRate is the share of input that came from cache. Returns 0 when
// nothing was read in.
func (u Usage) CacheHitRate() float64 {
	in := u.InputTokens + u.CacheReadTokens
	if in == 0 {
		return 0
	}
	return float64(u.CacheReadTokens) / float64(in)
}

// Response is what a provider returned, with what it cost.
type Response struct {
	Text  string
	Usage Usage
}

// EstimateTokens approximates a token count from text.
//
// Four characters per token is the usual rough ratio for English and code,
// and it is wrong in both directions: worse for non-Latin scripts, better for
// repetitive code. It exists so a provider that reports nothing still produces
// a comparable number, always flagged as Estimated. Never use it to make a
// billing claim.
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	// Whitespace runs collapse into few tokens, so counting bytes alone
	// overestimates padded text.
	compact := len(s) - countRuns(s, ' ') - countRuns(s, '\n')
	if compact < 1 {
		compact = 1
	}
	return compact/4 + 1
}

// EstimateMessages approximates the input size of a whole conversation.
func EstimateMessages(messages []Message) int {
	n := 0
	for _, m := range messages {
		n += EstimateTokens(m.Content) + EstimateTokens(m.Role)
		for _, img := range m.Images {
			// A rough, deliberately conservative constant: image tokenisation
			// differs per provider and is not derivable from byte size.
			n += 800 + len(img.Data)/2000
		}
	}
	return n
}

func countRuns(s string, c byte) int {
	n, prev := 0, byte(0)
	for i := 0; i < len(s); i++ {
		if s[i] == c && prev == c {
			n++
		}
		prev = s[i]
	}
	return n
}

// TrimForLog shortens text for display without pretending it is complete.
func TrimForLog(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
