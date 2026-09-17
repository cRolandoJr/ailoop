// Package tools gives an agent a way to look at the ground instead of
// imagining it, without giving it the run of the machine.
//
// The model of AI_LOOP 18.16 applies here: capability, permission and
// authority are three different things. A capability existing in this package
// never means an agent may use it; a Registry decides that, per phase.
//
// The request protocol is plain text on purpose. Native function calling
// differs between providers and some local models do not have it at all;
// AI_LOOP 15 requires the workflow to survive a change of model, so the
// protocol must be one that any model that can emit text can speak.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cRolandoJr/ailoop/internal/llm"
	"github.com/cRolandoJr/ailoop/internal/mcp"
	"github.com/cRolandoJr/ailoop/internal/patch"
	"github.com/cRolandoJr/ailoop/internal/verify"
)

type Capability string

const (
	// FSRead reads one file inside the workspace.
	FSRead Capability = "fs.read"
	// FSList lists the files of a directory inside the workspace.
	FSList Capability = "fs.list"
	// FSReadImage reads an image file so the model can look at it. Offered
	// only when the selected model actually reports vision: a capability the
	// provider does not have must not appear in an agent's menu.
	FSReadImage Capability = "fs.read_image"
	// FSGrep searches file contents by regular expression.
	FSGrep Capability = "fs.grep"
	// FSGlob lists files matching a path pattern.
	FSGlob Capability = "fs.glob"
	// MCPDescribe returns the full input schema of one MCP tool. It exists so
	// the schemas do not have to live in the prompt: the agent asks for the
	// one it intends to use.
	MCPDescribe Capability = "mcp.describe"
	// MCPCall invokes an MCP tool.
	MCPCall Capability = "mcp.call"
	// CmdRun runs one of the project's declared verification commands.
	// It is never an arbitrary shell: only what the project itself declared.
	CmdRun Capability = "cmd.run"
)

// All is every capability this package implements, in the order they are
// offered to a model.
var All = []Capability{FSRead, FSList, FSGlob, FSGrep, FSReadImage, MCPDescribe, MCPCall, CmdRun}

// Request is one tool invocation asked for by the model.
type Request struct {
	Cap Capability
	Arg string
	// Raw is the block as the model wrote it, for error messages.
	Raw string
}

// Result is what the workspace answered.
type Result struct {
	Request Request
	Allowed bool
	Output  string
	// Images is visual content the model should see alongside Output.
	Images []llm.Image
	Err    error
}

// Registry says which capabilities an agent may use right now, and with what
// limits. It is descriptive of policy, not of what is technically possible:
// the code below can always read a file; the Registry decides if it may.
type Registry struct {
	Allowed map[Capability]bool
	// RunnableCmds are the only commands CmdRun will execute, by name. They
	// come from the project's own verification config, so the agent cannot
	// invent a command that nobody declared.
	RunnableCmds map[string]string
	// MaxBytes caps how much a single read returns.
	MaxBytes int
	// MCP is the pool of connected tool servers, or nil when none are
	// configured.
	MCP *mcp.Pool
}

const defaultMaxBytes = 60000

func (r *Registry) permits(c Capability) bool {
	if r == nil || r.Allowed == nil {
		return false
	}
	return r.Allowed[c]
}

// Parse extracts tool requests from a model's reply.
//
// Format:
//
//	<<TOOL>>
//	fs.read: internal/patch/apply.go
//	<<END>>
func Parse(text string) []Request {
	var reqs []Request

	parts := strings.Split(text, "<<TOOL>>")
	for _, part := range parts[1:] {
		end := strings.Index(part, "<<END>>")
		if end == -1 {
			continue // unterminated block: ignored rather than guessed at
		}
		body := strings.TrimSpace(part[:end])

		colon := strings.Index(body, ":")
		if colon == -1 {
			continue
		}
		cap := Capability(strings.TrimSpace(body[:colon]))
		arg := strings.TrimSpace(body[colon+1:])
		if cap == "" || arg == "" {
			continue
		}

		reqs = append(reqs, Request{Cap: cap, Arg: arg, Raw: body})
	}

	return reqs
}

// Execute runs one request if the Registry permits it. A refusal is a normal
// result, not an error: the model is told why, and can continue.
func Execute(ctx context.Context, workspace string, reg *Registry, req Request) Result {
	res := Result{Request: req}

	if !reg.permits(req.Cap) {
		res.Output = fmt.Sprintf("REFUSED: capability %q is not permitted in this phase", req.Cap)
		return res
	}
	res.Allowed = true

	switch req.Cap {
	case FSRead:
		res.Output, res.Err = readFile(workspace, req.Arg, reg.maxBytes())
	case FSList:
		res.Output, res.Err = listDir(workspace, req.Arg)
	case FSGlob:
		res.Output, res.Err = Glob(workspace, req.Arg)
	case FSGrep:
		pattern, glob := splitGrepArg(req.Arg)
		res.Output, res.Err = Grep(workspace, pattern, glob)
	case MCPDescribe:
		if reg.MCP == nil {
			res.Output, res.Err = "", errNoMCP
		} else {
			res.Output, res.Err = reg.MCP.Describe(req.Arg)
		}
	case MCPCall:
		if reg.MCP == nil {
			res.Output, res.Err = "", errNoMCP
		} else {
			name, args, err := parseMCPCall(req.Arg)
			if err != nil {
				res.Err = err
			} else {
				res.Output, res.Err = reg.MCP.Call(ctx, name, args)
			}
		}
	case FSReadImage:
		var img llm.Image
		img, res.Err = readImage(workspace, req.Arg)
		if res.Err == nil {
			res.Images = append(res.Images, img)
			res.Output = fmt.Sprintf("(image attached: %s, %s, %d bytes)", req.Arg, img.MediaType, len(img.Data))
		}
	case CmdRun:
		res.Output, res.Err = runDeclared(ctx, workspace, reg, req.Arg)
	default:
		res.Allowed = false
		res.Output = fmt.Sprintf("REFUSED: unknown capability %q", req.Cap)
	}

	if res.Err != nil {
		res.Output = "ERROR: " + res.Err.Error()
	}
	return res
}

var errNoMCP = errors.New("no MCP servers are connected")

// parseMCPCall accepts "<server>.<tool> {json arguments}". The arguments are
// JSON because that is what the tool's schema describes; inventing a flatter
// syntax here would mean translating it back, badly.
func parseMCPCall(arg string) (name string, args map[string]any, err error) {
	arg = strings.TrimSpace(arg)
	brace := strings.Index(arg, "{")
	if brace == -1 {
		// No arguments at all is legitimate for a zero-parameter tool.
		return arg, map[string]any{}, nil
	}
	name = strings.TrimSpace(arg[:brace])
	if name == "" {
		return "", nil, errors.New("missing tool name before the JSON arguments")
	}
	if err := json.Unmarshal([]byte(arg[brace:]), &args); err != nil {
		return "", nil, fmt.Errorf("arguments must be a JSON object: %w", err)
	}
	return name, args, nil
}

// splitGrepArg accepts "<regexp>" or "<regexp> -- <glob>".
func splitGrepArg(arg string) (pattern, glob string) {
	if i := strings.Index(arg, " -- "); i != -1 {
		return strings.TrimSpace(arg[:i]), strings.TrimSpace(arg[i+4:])
	}
	return arg, ""
}

func (r *Registry) maxBytes() int {
	if r.MaxBytes > 0 {
		return r.MaxBytes
	}
	return defaultMaxBytes
}

func readFile(workspace, arg string, max int) (string, error) {
	rel, err := patch.SafeRelPath(workspace, arg)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(workspace, rel))
	if err != nil {
		return "", err
	}
	if len(data) > max {
		return string(data[:max]) + "\n... (truncated)", nil
	}
	return string(data), nil
}

func listDir(workspace, arg string) (string, error) {
	rel, err := patch.SafeRelPath(workspace, arg)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(filepath.Join(workspace, rel))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, e := range entries {
		if e.IsDir() {
			fmt.Fprintf(&b, "%s/\n", e.Name())
		} else {
			fmt.Fprintf(&b, "%s\n", e.Name())
		}
	}
	return b.String(), nil
}

func runDeclared(ctx context.Context, workspace string, reg *Registry, name string) (string, error) {
	cmd, ok := reg.RunnableCmds[name]
	if !ok {
		return "", fmt.Errorf("%q is not a command this project declared; available: %s",
			name, strings.Join(declaredNames(reg), ", "))
	}

	results := verify.Run(ctx, workspace, []verify.Check{{Name: name, Cmd: cmd}}, 0)
	if len(results) == 0 {
		return "", fmt.Errorf("command produced no result")
	}
	r := results[0]

	status := "PASSED"
	if !r.Passed {
		status = fmt.Sprintf("FAILED (exit %d)", r.ExitCode)
	}
	return fmt.Sprintf("$ %s\n%s\n\n%s", r.Cmd, r.Output, status), nil
}

func declaredNames(reg *Registry) []string {
	var names []string
	for n := range reg.RunnableCmds {
		names = append(names, n)
	}
	return names
}

// Protocol is the instruction block appended to an agent's system prompt so it
// knows the tools exist and what it may do with them.
func Protocol(reg *Registry) string {
	var allowed []string
	for _, c := range All {
		if reg.permits(c) {
			allowed = append(allowed, string(c))
		}
	}
	if len(allowed) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\nYou can inspect the real project instead of guessing. To do so, emit:\n")
	b.WriteString("<<TOOL>>\n<capability>: <argument>\n<<END>>\n\n")
	b.WriteString("Capabilities available to you in this phase: " + strings.Join(allowed, ", ") + "\n\n")
	if reg.permits(FSGrep) {
		b.WriteString("fs.grep takes a Go regular expression, optionally narrowed with \" -- <glob>\".\n")
		b.WriteString("  example: fs.grep: func Normalizar -- **/*.go\n")
	}
	if reg.permits(FSGlob) {
		b.WriteString("fs.glob takes a path pattern where ** crosses directories.\n")
		b.WriteString("  example: fs.glob: internal/**/*_test.go\n")
	}
	if reg.permits(FSReadImage) {
		b.WriteString("fs.read_image attaches an image file for you to look at (png, jpeg, gif, webp).\n")
	}
	if reg.permits(MCPCall) && reg.MCP != nil && !reg.MCP.Empty() {
		b.WriteString("\nExternal tool servers are available. Their tools:\n")
		b.WriteString(reg.MCP.Catalog())
		b.WriteString("\nThe list above is a menu, not the full interface. Before calling one,\n")
		b.WriteString("use mcp.describe to get its input schema:\n")
		b.WriteString("  mcp.describe: codegraph.find_code\n")
		b.WriteString("Then call it with JSON arguments:\n")
		b.WriteString("  mcp.call: codegraph.find_code {\"query\": \"where is Normalizar used\"}\n")
	}
	if reg.permits(CmdRun) {
		b.WriteString("cmd.run accepts only these declared commands: " + strings.Join(declaredNames(reg), ", ") + "\n")
	}
	b.WriteString("\nRules:\n")
	b.WriteString("- Ask for a file before describing its contents. Do not invent code you have not read.\n")
	b.WriteString("- Search before claiming something does not exist. An absence is a claim about the whole workspace, so it needs a sweep, not a guess.\n")
	b.WriteString("- You may emit several blocks at once; you will get every answer back.\n")
	b.WriteString("- When you have enough information, answer normally with no tool blocks.\n")
	return b.String()
}
