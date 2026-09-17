package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cRolandoJr/ailoop/internal/llm"
)

type CriticResponse struct {
	Pass     bool   `json:"pass"`
	Feedback string `json:"feedback"`
	// Usage is what this critique cost. The critic is a real call and can run
	// three times per phase: leaving it out of the ledger would make the loop
	// look cheaper than it is.
	Usage llm.Usage `json:"-"`
}

// EvaluateProposal uses an adversarial agent to critique a design proposal
func EvaluateProposal(ctx context.Context, task, proposal string, client llm.Client) (*CriticResponse, error) {
	sysPrompt := `You are the Critic Agent. Your job is to aggressively audit the proposed Design for the given task.
Look for contradictions, missing edge cases, security flaws, or bad architectural decisions.
Respond ONLY in valid JSON format:
{
	"pass": boolean, 
	"feedback": "string explaining exactly what needs to be fixed. Leave empty if pass is true."
}`

	messages := []llm.Message{
		{Role: "system", Content: sysPrompt},
		{Role: "user", Content: fmt.Sprintf("Task:\n%s\n\nProposed Design:\n%s\n", task, proposal)},
	}

	resp, err := client.Generate(ctx, messages)
	if err != nil {
		return nil, err
	}
	respStr := resp.Text

	// Try to extract JSON if LLM added markdown formatting
	jsonStr := respStr
	jsonStr = strings.TrimPrefix(jsonStr, "```json\n")
	jsonStr = strings.TrimPrefix(jsonStr, "```\n")
	jsonStr = strings.TrimSuffix(jsonStr, "\n```")
	jsonStr = strings.TrimSpace(jsonStr)

	var criticResp CriticResponse
	if err := json.Unmarshal([]byte(jsonStr), &criticResp); err != nil {
		// Fallback: If it failed to parse JSON, assume it failed and pass the raw text as feedback
		return &CriticResponse{
			Pass:     false,
			Feedback: "Critic failed to output valid JSON. Raw output: " + respStr,
			Usage:    resp.Usage,
		}, nil
	}

	criticResp.Usage = resp.Usage
	return &criticResp, nil
}
