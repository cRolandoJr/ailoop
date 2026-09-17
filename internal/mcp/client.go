package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// DefaultTimeout bounds a single call. A server that hangs must not hang the
// loop; a tool that never answers is a failed tool, not a paused workflow.
const DefaultTimeout = 60 * time.Second

// Server is one MCP server the workflow can talk to.
type Server struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	// Tools optionally restricts which of the server's tools are exposed.
	// Empty means all of them - see the cost note in Catalog.
	Tools []string `json:"tools,omitempty"`
}

// Client is a live connection to one MCP server process.
type Client struct {
	Server Server

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	out    *bufio.Reader
	mu     sync.Mutex
	nextID int

	tools    []Tool
	serverID string
}

// Connect starts the server process and completes the MCP handshake.
func Connect(ctx context.Context, s Server) (*Client, error) {
	cmd := exec.CommandContext(ctx, s.Command, s.Args...)
	if len(s.Env) > 0 {
		cmd.Env = cmd.Environ()
		for k, v := range s.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// stderr is left attached to the parent: an MCP server that cannot start
	// says so on stderr, and swallowing it turns a clear failure into a
	// mysterious timeout.
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not start %q: %w", s.Command, err)
	}

	c := &Client{Server: s, cmd: cmd, stdin: stdin, out: bufio.NewReaderSize(stdout, 1<<20)}

	var init initializeResult
	if err := c.call(ctx, "initialize", initializeParams{
		ProtocolVersion: protocolVersion,
		ClientInfo:      clientInfo{Name: "ailoop", Version: "0.1"},
	}, &init); err != nil {
		c.Close()
		return nil, fmt.Errorf("handshake with %q failed: %w", s.Name, err)
	}
	c.serverID = init.ServerInfo.Name

	if err := c.notify("notifications/initialized", struct{}{}); err != nil {
		c.Close()
		return nil, err
	}

	var list listToolsResult
	if err := c.call(ctx, "tools/list", struct{}{}, &list); err != nil {
		c.Close()
		return nil, fmt.Errorf("listing tools of %q failed: %w", s.Name, err)
	}
	c.tools = filterTools(list.Tools, s.Tools)

	return c, nil
}

func filterTools(all []Tool, keep []string) []Tool {
	if len(keep) == 0 {
		return all
	}
	want := map[string]bool{}
	for _, n := range keep {
		want[n] = true
	}
	var out []Tool
	for _, t := range all {
		if want[t.Name] {
			out = append(out, t)
		}
	}
	return out
}

// Tools returns the tools this server exposes to the workflow.
func (c *Client) Tools() []Tool { return c.tools }

// ServerName is what the server calls itself, which may differ from the
// configured name.
func (c *Client) ServerName() string { return c.serverID }

// Call invokes a tool and returns its text content.
func (c *Client) Call(ctx context.Context, tool string, args map[string]any) (string, error) {
	var res callResult
	if err := c.call(ctx, "tools/call", callParams{Name: tool, Arguments: args}, &res); err != nil {
		return "", err
	}

	var b strings.Builder
	for _, blk := range res.Content {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
		}
	}
	if res.IsError {
		return b.String(), fmt.Errorf("tool %q reported an error", tool)
	}
	return b.String(), nil
}

func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()

	req := request{JSONRPC: "2.0", ID: &id, Method: method, Params: params}
	if err := c.write(req); err != nil {
		return err
	}

	// Read until the response with our ID arrives. Servers interleave
	// notifications, and treating the first line as the answer reads a log
	// message as a result.
	deadline := time.Now().Add(DefaultTimeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %q", method)
		}
		line, err := c.out.ReadBytes('\n')
		if err != nil {
			return fmt.Errorf("reading from %q: %w", c.Server.Name, err)
		}
		line = trimSpace(line)
		if len(line) == 0 {
			continue
		}

		var resp response
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // not JSON-RPC: server noise, skip
		}
		if resp.ID == nil || *resp.ID != id {
			continue // a notification or another call's answer
		}
		if resp.Error != nil {
			return resp.Error
		}
		if out == nil || len(resp.Result) == 0 {
			return nil
		}
		return json.Unmarshal(resp.Result, out)
	}
}

func (c *Client) notify(method string, params any) error {
	return c.write(request{JSONRPC: "2.0", Method: method, Params: params})
}

func (c *Client) write(req request) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.stdin.Write(append(data, '\n'))
	return err
}

// Close shuts the server process down.
func (c *Client) Close() error {
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	return nil
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t') {
		b = b[1:]
	}
	return b
}
