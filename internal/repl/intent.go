// Package repl decides what a typed line means. It does no input and no
// output: the terminal loop lives elsewhere, so this rule can be tested
// without driving a keyboard.
package repl

import (
	"sort"
	"strings"
)

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
	// KindNeedsWork is a command that has nothing to work on yet.
	KindNeedsWork
)

// Info is one command as help shows it.
type Info struct {
	Name string
	Desc string
}

// Catalog lists the commands with what each one does, in help order.
//
// It carries the descriptions because a list of bare names makes a person run
// each one to find out what it is - which is the opposite of help.
func Catalog() []Info {
	out := make([]Info, 0, len(commands)+2)
	for n, c := range commands {
		out = append(out, Info{Name: n, Desc: c.desc})
	}
	// help and exit are answered by the session itself, so they are not in the
	// table, but leaving them out of the help would hide the way out.
	out = append(out,
		Info{Name: "help", Desc: "this list"},
		Info{Name: "exit", Desc: "leave the session"})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

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
		if name == "" {
			// "/" on its own is someone asking what there is, not a command
			// whose name is empty. Answering "unknown command" to that is the
			// worst possible end for a person who was exploring.
			name = "help"
		}
		if name == "exit" || name == "quit" {
			return Intent{Kind: KindExit}
		}
		// Some commands read the work item. Delegating them with nothing
		// started reaches an error written for the shell - "run ailoop
		// start" - given to someone already inside the session, where typing
		// the task IS starting it.
		if c, known := commands[name]; known && c.needsWork && !workStarted {
			return Intent{Kind: KindNeedsWork, Text: name}
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
	c, ok := commands[name]
	return c.cli, ok
}

// command is one entry of the session's surface.
//
// needsWork records whether the command reads the work item. It is measured,
// not guessed: those are the use cases that load state, and verify is not one
// of them - running the project's checks needs a project, not a loop.
type command struct {
	cli       string
	desc      string
	needsWork bool
}

var commands = map[string]command{
	"status":       {"status", "where the work is: phase, versions, pending documents", true},
	"cost":         {"cost", "tokens and budget spent so far, by phase", true},
	"decisions":    {"decisions", "the decision record of this work item", true},
	"history":      {"history", "every phase transition and what was approved", true},
	"undo":         {"undo", "put back the files the last approved patch changed", true},
	"verify":       {"verify", "run the checks this project declared", false},
	"doctor":       {"doctor", "what this machine offers", false},
	"capabilities": {"capabilities", "what the model and this environment can do, by phase", false},
	"mcp":          {"mcp", "the external tool servers configured here", false},
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
