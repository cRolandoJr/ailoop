package agents

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cRolandoJr/ailoop/internal/llm"
	"github.com/cRolandoJr/ailoop/internal/mcp"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/tools"
	"github.com/cRolandoJr/ailoop/internal/web"
)

// MaxToolRounds bounds how many times an agent may look at the workspace
// before it has to answer. Without a bound, a model that keeps asking for one
// more file never produces anything.
const MaxToolRounds = 6

// ToolObserver is notified of each tool request, so the CLI can show the user
// exactly what the agent looked at. An agent inspecting the workspace behind
// the user's back would trade one opacity for another.
type ToolObserver func(res tools.Result)

// RunPhase executes the appropriate agent for the current phase.
//
// workspace and declaredCmds enable the tool loop: the agent may inspect the
// real project instead of describing one it imagined. reg decides what it is
// allowed to inspect.
func RunPhase(ctx context.Context, s *state.AIState, projectContext string, client llm.Client,
	workspace string, declaredCmds map[string]string, pool *mcp.Pool, fetcher *web.Fetcher,
	observe ToolObserver) (string, error) {

	sysPrompt := getSystemPromptForPhase(s.CurrentPhase)
	if sysPrompt == "" {
		return "", fmt.Errorf("no agent defined for phase %s", s.CurrentPhase)
	}

	// The research agent is reachable only through this callback. The primary
	// agent never holds the network itself.
	var research func(context.Context, string) (string, error)
	if fetcher != nil {
		research = func(ctx context.Context, question string) (string, error) {
			return Research(ctx, question, client, fetcher, observe)
		}
	}
	reg := RegistryFor(s.CurrentPhase, declaredCmds, client.Describe(), pool, research)
	sysPrompt += tools.Protocol(reg)

	// Build the context for the LLM based on current state
	contextMsg := fmt.Sprintf("Task: %s\n", s.TaskDescription)

	if s.Spec.Content != "" {
		contextMsg += fmt.Sprintf("Spec (v%d):\n%s\n", s.Spec.Version, s.Spec.Content)
	}
	if s.Design.Content != "" {
		contextMsg += fmt.Sprintf("Design (v%d):\n%s\n", s.Design.Version, s.Design.Content)
	}
	if s.Plan.Content != "" {
		contextMsg += fmt.Sprintf("Plan (v%d):\n%s\n", s.Plan.Version, s.Plan.Content)
	}

	// Approved decisions are authoritative. An agent that never sees them can
	// contradict them without knowing; one that sees them cannot claim it did.
	if brief := s.Record.Brief(); brief != "" {
		contextMsg += "\n" + brief
	}

	if projectContext != "" {
		contextMsg += fmt.Sprintf("\n<PROJECT_CONTEXT>\n%s\n</PROJECT_CONTEXT>\n", projectContext)
	}

	if s.RejectionReason != "" {
		contextMsg += fmt.Sprintf("\nWARNING: Your previous proposal was REJECTED by the user for the following reason:\n\"%s\"\nPlease fix the divergence and propose version %d.\n", s.RejectionReason, getCurrentVersion(s))
	}

	messages := []llm.Message{
		{Role: "system", Content: sysPrompt, Round: -1},
		{Role: "user", Content: contextMsg + "\nPlease proceed with your phase.", Round: -1},
	}

	// Tool loop: generate, honour any inspection the agent asked for, feed the
	// real answers back, and let it decide again.
	for round := 0; ; round++ {
		// Elide observations the agent has already reasoned past. Cheap when
		// there is nothing to elide, and it is what keeps a long exploration
		// from re-paying for everything it already read.
		if saved := maskOldObservations(messages, round); saved > 0 && observe != nil {
			observe(tools.Result{
				Request: tools.Request{Cap: "context.mask"},
				Allowed: true,
				Output:  fmt.Sprintf("elided ~%d tokens of earlier observations", saved),
			})
		}

		// Provide visual newline separation for stream output
		fmt.Printf("\n\n---\n[%s Agent - Round %d]\n\n", s.CurrentPhase, round+1)

		resp, err := client.GenerateStream(ctx, messages, func(chunk string) {
			fmt.Print(chunk)
		})
		fmt.Println() // Newline after stream finishes

		if err != nil {
			return "", err
		}
		s.Spend.Record(s.CurrentPhase, resp.Usage)
		reply := resp.Text

		reqs := tools.Parse(reply)

		// Citation Check
		if invalidCitations := checkCitations(workspace, reply); len(invalidCitations) > 0 {
			errStr := "Citation Error: You cited files or line ranges that do not exist:\n" + strings.Join(invalidCitations, "\n") + "\nPlease correct your citations or use fs.read to check the file contents first."
			messages = append(messages,
				llm.Message{Role: "assistant", Content: reply, Round: round},
				llm.Message{Role: "user", Content: errStr, Round: round},
			)
			continue
		}

		if len(reqs) == 0 {
			extractDecisions(s, reply)
			return reply, nil
		}

		if round >= MaxToolRounds {
			// Give the model its observations but stop the loop: it must answer.
			messages = append(messages,
				llm.Message{Role: "assistant", Content: reply, Round: round},
				llm.Message{Role: "user", Content: "Tool budget exhausted. Answer now with what you have, and state explicitly anything you could not verify.", Round: round},
			)
			last, err := client.Generate(ctx, messages)
			if err != nil {
				return "", err
			}
			s.Spend.Record(s.CurrentPhase, last.Usage)
			return last.Text, nil
		}

		var observations strings.Builder
		var images []llm.Image
		observations.WriteString("Results of your tool requests:\n\n")
		for _, req := range reqs {
			res := tools.Execute(ctx, workspace, reg, req)
			if observe != nil {
				observe(res)
			}
			images = append(images, res.Images...)
			fmt.Fprintf(&observations, "<<RESULT %s: %s>>\n%s\n<<END RESULT>>\n\n", req.Cap, req.Arg, res.Output)
		}

		messages = append(messages,
			llm.Message{Role: "assistant", Content: reply, Round: round},
			llm.Message{Role: "user", Content: observations.String(), Images: images, Round: round},
		)
	}
}

func getSystemPromptForPhase(phase state.Phase) string {
	// ADAPTIVE TEACHING: Injected strictly in the base prompts
	baseTeaching := "\n\nThe user is a Junior developer. Do not use 'black magic' or overly complex abstractions unless necessary. Briefly explain the design patterns used and WHY you chose this approach so the user can learn."

	switch phase {
	case state.PhaseDiscovery:
		return `You are the Discovery Agent. Your job is to understand the user's task and identify any major decisions needed. 
Produce a concise summary of what needs to be done. Do NOT write code.
If you make an architectural decision, emit it using this exact format anywhere in your response:
<<<< DECISION
Statement: <short binding decision>
Rationale: <why it was chosen>
>>>>`
	case state.PhaseDesign:
		return `You are the Design Agent. Your job is to take the Discovery summary and produce a clear Design and Test Strategy.
Output a concise design. Do NOT write the actual implementation code yet.
If you make a binding design decision, emit it using this exact format anywhere in your response:
<<<< DECISION
Statement: <short binding decision>
Rationale: <why it was chosen>
>>>>` + baseTeaching
	case state.PhasePlan:
		return `You are the Plan Agent. Your job is to take the approved Design and produce a step-by-step checklist of tasks required to implement it.
Format the output as a Markdown checklist. Do NOT write the implementation code yet.` + baseTeaching
	case state.PhaseImplementation:
		return `You are the Implementation Agent. Your job is to write the code based on the approved Design and Project Context provided.
Output the code changes required. 
CRITICAL: read a file with fs.read BEFORE proposing any change to it. A patch for a
file you have not read will be refused: its search text would be invented.
To modify files, you MUST use the following exact block format for every change:
<<<<
path/to/file.ext
====
exact existing lines to be replaced (including leading whitespace)
====
new lines to replace them with
>>>>
` + baseTeaching
	case state.PhaseVerification:
		return `You are the Verification Agent. Review the code changes and verify they meet the Design.`
	default:
		return ""
	}
}

func getCurrentVersion(s *state.AIState) int {
	switch s.CurrentPhase {
	case state.PhaseDiscovery:
		return s.Spec.Version
	case state.PhaseDesign:
		return s.Design.Version
	case state.PhasePlan:
		return s.Plan.Version
	case state.PhaseImplementation, state.PhaseVerification:
		return s.Decisions.Version
	}
	return 1
}

func extractDecisions(s *state.AIState, text string) {
	parts := strings.Split(text, "<<<< DECISION\n")
	if len(parts) <= 1 {
		return
	}

	for _, part := range parts[1:] {
		endIdx := strings.Index(part, ">>>>")
		if endIdx == -1 {
			continue
		}

		block := part[:endIdx]
		lines := strings.Split(block, "\n")

		var statement, rationale string
		for _, line := range lines {
			if strings.HasPrefix(line, "Statement:") {
				statement = strings.TrimSpace(strings.TrimPrefix(line, "Statement:"))
			} else if strings.HasPrefix(line, "Rationale:") {
				rationale = strings.TrimSpace(strings.TrimPrefix(line, "Rationale:"))
			}
		}

		if statement != "" {
			s.Record.Add(statement, rationale, nil, s.CurrentPhase)
		}

	}
}

var citationRegex = regexp.MustCompile(`\[([^:]+):(\d+)(?:-(\d+))?\]`)

func checkCitations(workspace, text string) []string {
	var invalid []string
	matches := citationRegex.FindAllStringSubmatch(text, -1)

	for _, match := range matches {
		if len(match) < 3 {
			continue
		}

		filePath := match[1]
		// Prevent absolute paths or directory traversal in citations
		if filepath.IsAbs(filePath) || strings.Contains(filePath, "..") {
			invalid = append(invalid, fmt.Sprintf("%s: unsafe path", match[0]))
			continue
		}

		fullPath := filepath.Join(workspace, filePath)
		stat, err := os.Stat(fullPath)
		if err != nil || stat.IsDir() {
			invalid = append(invalid, fmt.Sprintf("%s: file not found", match[0]))
			continue
		}

		// Optional: We could check if line numbers are out of bounds by reading the file
		// but to keep it fast, we just check file existence for now.
	}

	return invalid
}
