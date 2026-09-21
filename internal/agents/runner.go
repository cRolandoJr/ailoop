package agents

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

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
// PhaseInput is everything a phase agent needs.
//
// It replaced eight positional parameters. A function that takes eight things
// is one where the caller gets the order wrong eventually, and where adding a
// ninth touches every call site.
type PhaseInput struct {
	State          *state.AIState
	Client         llm.Client
	Workspace      string
	ProjectContext string
	// Attachments are images the person attached with @references. They are
	// only sent when the model reports vision.
	Attachments  []llm.Image
	DeclaredCmds map[string]string
	Pool         *mcp.Pool
	Fetcher      *web.Fetcher
	Observe      ToolObserver
	LSP          interface {
		Definition(ctx context.Context, path string, line, char int) (string, error)
		References(ctx context.Context, path string, line, char int) (string, error)
	}
}

// RunPhase executes the agent of the current phase.
func RunPhase(ctx context.Context, in PhaseInput) (out string, err error) {
	s := in.State
	client := in.Client
	projectContext := in.ProjectContext
	workspace := in.Workspace
	declaredCmds := in.DeclaredCmds
	pool := in.Pool
	fetcher := in.Fetcher
	observe := in.Observe

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
	// One journal line per phase run. Spend aggregates by phase for the budget;
	// this is the grain that answers what a given agent did to the workspace,
	// which the aggregate cannot. Written from a defer so every exit records,
	// including the ones that return an error.
	caps := client.Describe()
	turn := state.Turn{Phase: string(s.CurrentPhase), Provider: caps.Provider, Model: caps.Model}
	started := time.Now()
	defer func() {
		turn.DurationSeconds = time.Since(started).Seconds()
		if err != nil {
			turn.Err = err.Error()
		}
		state.AppendTurn(workspace, turn)
	}()

	reg := RegistryFor(s.CurrentPhase, declaredCmds, caps, pool, research, in.LSP)
	sysPrompt += tools.Protocol(reg, workspace)

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

	if s.Feedback != "" {
		contextMsg += fmt.Sprintf("\nWARNING: Your previous proposal was REJECTED by the user for the following reason:\n\"%s\"\nPlease fix the divergence and propose version %d.\n", s.Feedback, getCurrentVersion(s))
	}

	messages := []llm.Message{
		{Role: "system", Content: sysPrompt, Round: -1},
		{
			Role:    "user",
			Content: contextMsg + "\nPlease proceed with your phase.",
			Images:  visibleTo(client, in.Attachments),
			Round:   -1,
		},
	}

	// What has already been asked, and in which round. A phase has seven
	// rounds; measured, an agent spent four of them asking for the same
	// missing file, because the answer never said it was the same answer.
	askedIn := map[string]int{}

	// Tool loop: generate, honour any inspection the agent asked for, feed the
	// real answers back, and let it decide again.
	for round := 0; ; round++ {
		// Before spending, not after. A check that runs afterwards reports a
		// number already spent: that is a receipt, not a limit.
		if err := s.Budget.Check(&s.Spend); err != nil {
			return "", err
		}

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
		turn.Usage.Add(resp.Usage)
		turn.Rounds = round + 1
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
			// A block the model meant to send and Parse could not read is not
			// an answer. Ending the phase here left it believing it had asked
			// for a file, and left the person with a phase that closed in one
			// round having done nothing.
			if problem := tools.MalformedBlock(reply); problem != "" {
				if observe != nil {
					observe(tools.Result{
						Request: tools.Request{Cap: "tool.malformed"},
						Output:  problem,
					})
				}
				messages = append(messages,
					llm.Message{Role: "assistant", Content: reply, Round: round},
					llm.Message{Role: "user", Content: problem, Round: round},
				)
				continue
			}
			extractDecisions(s, reply)
			return reply, nil
		}

		if round >= MaxToolRounds {
			// Give the model its observations but stop the loop: it must answer.
			messages = append(messages,
				llm.Message{Role: "assistant", Content: reply, Round: round},
				llm.Message{Role: "user", Content: "Tool budget exhausted. Answer now with what you have, and state explicitly anything you could not verify.", Round: round},
			)
			if err := s.Budget.Check(&s.Spend); err != nil {
				return "", err
			}
			// Streamed like every other round: this is the one that carries
			// the answer, and it used to be the only one the person could not
			// watch arrive.
			last, err := client.GenerateStream(ctx, messages, func(chunk string) {
				fmt.Print(chunk)
			})
			fmt.Println()
			if err != nil {
				return "", err
			}
			s.Spend.Record(s.CurrentPhase, last.Usage)
			turn.Usage.Add(last.Usage)
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
			switch {
			case req.Cap == "cmd.run":
				turn.CommandsRun = append(turn.CommandsRun, req.Arg)
			case strings.HasPrefix(string(req.Cap), "fs."):
				turn.FilesRead = append(turn.FilesRead, req.Arg)
			}
			images = append(images, res.Images...)

			key := string(req.Cap) + "\x00" + req.Arg
			note := ""
			if before, repeated := askedIn[key]; repeated {
				// The answer has not changed and will not: nothing between
				// two rounds of the same conversation touched the workspace.
				note = fmt.Sprintf("You already asked for this in round %d and this is the same answer. "+
					"Asking again spends a round and changes nothing; do something else or answer now.\n", before+1)
			} else {
				askedIn[key] = round
			}

			fmt.Fprintf(&observations, "<<RESULT %s: %s>>\n%s%s\n<<END RESULT>>\n\n",
				req.Cap, req.Arg, note, res.Output)
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

	// LOCAL RAG: Ensure the agent knows how to search effectively
	localRAG := "\n\nCRITICAL (Local RAG): When exploring large codebases, do NOT blindly guess paths or ask for files without knowing they exist. Actively use `fs.grep` (to search by keywords) and `fs.glob`/`fs.list` (to find files) as your local Retrieval-Augmented Generation (RAG) system to index the project before reading files."

	switch phase {
	case state.PhaseDiscovery:
		return `You are the Discovery Agent. Your job is to understand the user's task and identify any major decisions needed. 
Produce a concise summary of what needs to be done. Do NOT write code.
If you make an architectural decision, emit it using this exact format anywhere in your response:
<<<< DECISION
Statement: <short binding decision>
Rationale: <why it was chosen>
>>>>` + localRAG
	case state.PhaseDesign:
		return `You are the Design Agent. Your job is to take the Discovery summary and produce a clear Design and Test Strategy.
Output a concise design. Do NOT write the actual implementation code yet.
If you make a binding design decision, emit it using this exact format anywhere in your response:
<<<< DECISION
Statement: <short binding decision>
Rationale: <why it was chosen>
>>>>` + baseTeaching + localRAG
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
To CREATE a new file, use the same block with the file path and the search
section left EMPTY (nothing between the ==== markers): an empty search on a
file that does not exist means "create it with these lines". There is no
separate tool for creating files.
` + baseTeaching + localRAG
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
	lines := map[string]int{}
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

		// A citation to line 900 of a 40-line file is exactly the hallucination
		// this check exists to catch: the file is real, the claim is not. The
		// count is cached per call because a verdict cites the same file often.
		total, ok := lines[fullPath]
		if !ok {
			total = countLines(fullPath)
			lines[fullPath] = total
		}
		if total < 0 {
			continue // unreadable after a successful Stat: do not guess
		}
		from, _ := strconv.Atoi(match[2])
		to := from
		if match[3] != "" {
			to, _ = strconv.Atoi(match[3])
		}
		if from < 1 || to < from || to > total {
			invalid = append(invalid, fmt.Sprintf("%s: line out of range (file has %d lines)", match[0], total))
		}
	}

	return invalid
}

// countLines reports how many lines a file has, or -1 when it cannot be read.
// A trailing newline ends the last line, it does not start an empty extra one:
// "a\nb\n" has two lines, and a citation to its line 3 must bounce.
func countLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	n := bytes.Count(data, []byte("\n"))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n
}

// visibleTo drops attached images when the model cannot see them. Sending an
// image to a model without vision costs tokens and returns a confident
// description of nothing.
func visibleTo(client llm.Client, images []llm.Image) []llm.Image {
	if len(images) == 0 || !client.Describe().Vision.OK() {
		return nil
	}
	return images
}
