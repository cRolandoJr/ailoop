// Package repl decides what a typed line means. It does no input and no
// output: the terminal loop lives elsewhere, so this rule can be tested
// without driving a keyboard.
package repl

import "strings"

// Kind is what the loop should do with a line.
type Kind int

const (
	// KindNothing is a line that asks for nothing, such as an empty line
	// before any work exists.
	KindNothing Kind = iota
	// KindStart begins a new work item; Text is the task.
	KindStart
	// KindAdvance runs the next phase. Text, when present, is feedback the
	// agent reads before proposing.
	KindAdvance
	// KindCommand is a slash command; Text is its name without the slash,
	// and Args is whatever followed it.
	KindCommand
	// KindExit leaves the session.
	KindExit
)

// Intent is the decision taken about one line.
type Intent struct {
	Kind Kind
	Text string
	Args string
}

// Interpret decides what a line means, given whether work is already under way.
//
// The rule it encodes is that typed text is always the same thing - something
// the person is telling the agent - and only its destination changes. With no
// work started the text is the task; with work under way it is feedback
// carried into the next phase. An empty line means "go on with what you have",
// which is the common case and therefore costs one keystroke.
//
// Slash is the escape hatch for everything that is not addressed to the agent,
// and it is reserved without exception: "/tmp/foo is broken" is read as a
// command too. Deciding by whether the name happens to be in the table would
// send a misspelled "/costo" to the agent as prose, and a command that fails
// by being quietly answered is worse than one that is refused by name. The
// price is paid in the other direction instead - an unknown name is reported.
func Interpret(line string, workStarted bool) Intent {
	line = strings.TrimSpace(line)

	if rest, isCommand := strings.CutPrefix(line, "/"); isCommand {
		name, args, _ := strings.Cut(rest, " ")
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "exit" || name == "quit" {
			return Intent{Kind: KindExit}
		}
		return Intent{Kind: KindCommand, Text: name, Args: strings.TrimSpace(args)}
	}

	if line == "" {
		if !workStarted {
			// Advancing here would ask the agent to work on nothing.
			return Intent{Kind: KindNothing}
		}
		return Intent{Kind: KindAdvance}
	}

	if !workStarted {
		return Intent{Kind: KindStart, Text: line}
	}
	return Intent{Kind: KindAdvance, Text: line}
}

// Command maps a slash command to the CLI command it runs, reporting whether
// the name is one this session knows.
//
// The pairing is a table rather than a switch on the caller's side so that a
// command reachable from the session is, by construction, a command the CLI
// accepts - a mapping to a name that run() does not handle would print
// "Unknown command" from inside the session.
func Command(name string) (cli string, known bool) {
	cmd, ok := commands[name]
	return cmd, ok
}

var commands = map[string]string{
	"status":       "status",
	"cost":         "cost",
	"decisions":    "decisions",
	"history":      "history",
	"undo":         "undo",
	"verify":       "verify",
	"doctor":       "doctor",
	"capabilities": "capabilities",
	"mcp":          "mcp",
}

// Names lists the known commands, for help text and completion.
func Names() []string {
	names := make([]string, 0, len(commands)+1)
	for n := range commands {
		names = append(names, n)
	}
	names = append(names, "exit")
	return names
}
