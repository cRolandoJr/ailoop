package agents

import (
	"fmt"
	"strings"

	"github.com/cRolandoJr/ailoop/internal/llm"
)

// The tool loop resends the whole conversation on every round. Ask for six
// files and the sixth request carries the previous five in full, so the cost
// grows with the square of the exploration. Masking replaces the bulk of an
// old observation with a line saying what it was and where it came from.
//
// Recovery needs no bookkeeping: the source is the workspace. If the agent
// needs that file again it asks for it again, and only then does it pay.

const (
	// MaskAfterRounds keeps recent observations intact. An agent is usually
	// still reasoning over what it just read.
	MaskAfterRounds = 2

	// MaskThresholdTokens is when masking starts at all. Below it the
	// conversation is small, and rewriting earlier messages would throw away
	// a cached prefix to save tokens that were not a problem - a trade that
	// only pays once the history is genuinely large.
	MaskThresholdTokens = 12000

	// MaskMinChars leaves short observations alone: the marker would cost
	// more than the content.
	MaskMinChars = 600
)

// maskOldObservations rewrites tool-result messages older than MaskAfterRounds
// into compact references, and reports how many tokens that saved.
func maskOldObservations(messages []llm.Message, currentRound int) (saved int) {
	if llm.EstimateMessages(messages) < MaskThresholdTokens {
		return 0
	}

	for i := range messages {
		m := &messages[i]
		if !isObservation(m) {
			continue
		}
		if m.Round < 0 || currentRound-m.Round <= MaskAfterRounds {
			continue // recent enough to still be in play
		}
		if len(m.Content) < MaskMinChars {
			continue
		}
		if mentionsFailure(m.Content) {
			// Never mask a failure while it may still be under diagnosis:
			// hiding the error breaks the loop that is trying to fix it.
			continue
		}

		before := llm.EstimateTokens(m.Content)
		m.Content = summarise(m.Content)
		m.Images = nil
		saved += before - llm.EstimateTokens(m.Content)
	}
	return saved
}

func isObservation(m *llm.Message) bool {
	return m.Role == "user" && strings.HasPrefix(m.Content, "Results of your tool requests:")
}

// mentionsFailure is deliberately broad. A false positive costs some tokens;
// a false negative costs the agent the error it was debugging.
func mentionsFailure(s string) bool {
	low := strings.ToLower(s)
	for _, needle := range []string{"error:", "failed", "fail\n", "refused:", "panic:", "exit 1", "no matches", "not found"} {
		if strings.Contains(low, needle) {
			return true
		}
	}
	return false
}

// summarise keeps what the observation was about and drops its body.
func summarise(content string) string {
	var refs []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "<<RESULT ") {
			head := strings.TrimPrefix(line, "<<RESULT ")
			head = strings.TrimSuffix(strings.TrimSpace(head), ">>")
			refs = append(refs, head)
		}
	}
	if len(refs) == 0 {
		refs = append(refs, "an earlier tool request")
	}

	return fmt.Sprintf(
		"[Earlier observations elided to save context: %s. "+
			"Nothing about them has changed. Request any of them again if you need the detail.]",
		strings.Join(refs, "; "))
}
