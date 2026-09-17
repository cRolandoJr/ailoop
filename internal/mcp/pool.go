package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Pool holds every connected MCP server and presents their tools as one
// namespace: "<server>.<tool>".
type Pool struct {
	clients map[string]*Client
	// failed records servers that could not be reached, so the user is told
	// instead of silently getting fewer capabilities than configured.
	failed map[string]error
}

// Open connects to every configured server. A server that fails to start is
// recorded, not fatal: losing one tool server should not stop the work.
func Open(ctx context.Context, servers []Server) *Pool {
	p := &Pool{clients: map[string]*Client{}, failed: map[string]error{}}
	for _, s := range servers {
		c, err := Connect(ctx, s)
		if err != nil {
			p.failed[s.Name] = err
			continue
		}
		p.clients[s.Name] = c
	}
	return p
}

func (p *Pool) Close() {
	for _, c := range p.clients {
		c.Close()
	}
}

// Failed returns the servers that could not be reached.
func (p *Pool) Failed() map[string]error { return p.failed }

// Count returns how many tools are available across all servers.
func (p *Pool) Count() int {
	n := 0
	for _, c := range p.clients {
		n += len(c.Tools())
	}
	return n
}

// Empty reports whether there is anything to offer at all.
func (p *Pool) Empty() bool { return p.Count() == 0 }

// Catalog renders one line per tool: qualified name and the first sentence of
// its description.
//
// This is the whole point of the deferred design. A server like CodeGraph
// advertises dozens of tools, each with a JSON Schema that runs to hundreds of
// tokens; pasting all of them into the system prompt costs thousands of tokens
// on EVERY request of EVERY round of the tool loop, whether or not the agent
// uses one. The catalog is a menu; the schema is fetched with mcp.describe
// only for the tool the agent actually intends to call.
func (p *Pool) Catalog() string {
	if p.Empty() {
		return ""
	}

	var names []string
	for name := range p.clients {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, server := range names {
		for _, t := range p.clients[server].Tools() {
			fmt.Fprintf(&b, "  %s.%s - %s\n", server, t.Name, firstSentence(t.Description))
		}
	}
	return b.String()
}

// Describe returns the full input schema of one tool, on demand.
func (p *Pool) Describe(qualified string) (string, error) {
	server, tool, err := split(qualified)
	if err != nil {
		return "", err
	}
	c, ok := p.clients[server]
	if !ok {
		return "", fmt.Errorf("no MCP server named %q (have: %s)", server, strings.Join(p.serverNames(), ", "))
	}
	for _, t := range c.Tools() {
		if t.Name == tool {
			return fmt.Sprintf("%s.%s\n%s\n\ninput schema:\n%s",
				server, t.Name, t.Description, string(t.InputSchema)), nil
		}
	}
	return "", fmt.Errorf("server %q has no tool %q", server, tool)
}

// Call invokes a qualified tool.
func (p *Pool) Call(ctx context.Context, qualified string, args map[string]any) (string, error) {
	server, tool, err := split(qualified)
	if err != nil {
		return "", err
	}
	c, ok := p.clients[server]
	if !ok {
		return "", fmt.Errorf("no MCP server named %q", server)
	}
	return c.Call(ctx, tool, args)
}

func (p *Pool) serverNames() []string {
	var out []string
	for n := range p.clients {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func split(qualified string) (server, tool string, err error) {
	i := strings.Index(qualified, ".")
	if i <= 0 || i == len(qualified)-1 {
		return "", "", fmt.Errorf("expected <server>.<tool>, got %q", qualified)
	}
	return qualified[:i], qualified[i+1:], nil
}

// firstSentence keeps the catalog to one line per tool.
func firstSentence(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if i := strings.Index(s, ". "); i != -1 {
		s = s[:i+1]
	}
	const max = 110
	if len(s) > max {
		s = s[:max] + "..."
	}
	if s == "" {
		return "(no description)"
	}
	return s
}

// Schemas renders every tool with its full input schema: what the naive
// design would have to paste into the system prompt on every request. It
// exists so the cost of that choice can be measured instead of assumed.
func (p *Pool) Schemas() string {
	var b strings.Builder
	for _, server := range p.serverNames() {
		for _, t := range p.clients[server].Tools() {
			fmt.Fprintf(&b, "%s.%s\n%s\n\ninput schema:\n%s\n\n",
				server, t.Name, t.Description, string(t.InputSchema))
		}
	}
	return b.String()
}
