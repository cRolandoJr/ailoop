// Package lsp speaks the Language Server Protocol to a project's language
// server, so an agent can ask where a symbol is defined and who uses it
// instead of inferring it from a grep.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Client is one language server and the conversation with it.
//
// A language server does not answer in turns: it interleaves notifications -
// progress, diagnostics, log lines - with the responses to requests, and it
// writes them whenever it wants. So a single goroutine owns the read side and
// routes each message by id, and callers wait on their own id. Reading
// straight off the stream inside a call returns whatever arrived first, which
// is usually someone else's message.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	// writeMu serialises the request side. Two goroutines interleaving their
	// framing would produce one unparseable message.
	writeMu sync.Mutex
	reqID   int32

	mu      sync.Mutex
	pending map[int32]chan response
	dead    error // set once the reader stops; nil while the server is alive
}

// response is one answer routed back to the caller that asked for it.
type response struct {
	result json.RawMessage
	err    error
}

// Start launches a language server and begins reading from it.
func Start(ctx context.Context, cmdName string, args ...string) (*Client, error) {
	cmd := exec.CommandContext(ctx, cmdName, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	c := newClient(stdin, stdout)
	c.cmd = cmd
	return c, nil
}

// newClient wires a client to an already-open pair of streams and starts the
// reader. Start uses it with the subprocess pipes; tests use it with a
// scripted server, so the double cuts at the process boundary.
func newClient(stdin io.WriteCloser, stdout io.ReadCloser) *Client {
	c := &Client{
		stdin:   stdin,
		stdout:  stdout,
		pending: map[int32]chan response{},
	}
	go c.read()
	return c
}

// read owns the stream for the client's whole life. Nothing else reads it.
func (c *Client) read() {
	r := bufio.NewReader(c.stdout)
	for {
		body, err := readMessage(r)
		if err != nil {
			c.shutdownPending(err)
			return
		}

		var msg struct {
			ID     *int32          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &msg); err != nil {
			// A malformed message is not a reason to abandon the stream: the
			// next one may be the answer someone is waiting for.
			continue
		}
		// No id means a notification or a server-initiated request. Neither
		// is anyone's answer.
		if msg.ID == nil {
			continue
		}

		res := response{result: msg.Result}
		if msg.Error != nil {
			res.err = fmt.Errorf("lsp: %s", msg.Error.Message)
		}

		c.mu.Lock()
		ch, waiting := c.pending[*msg.ID]
		delete(c.pending, *msg.ID)
		c.mu.Unlock()

		if waiting {
			ch <- res
		}
	}
}

// shutdownPending releases every caller still waiting when the stream ends.
// Without it they would wait for a server that is gone.
func (c *Client) shutdownPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dead == nil {
		c.dead = err
	}
	for id, ch := range c.pending {
		ch <- response{err: err}
		delete(c.pending, id)
	}
}

// readMessage reads one header-framed message body.
func readMessage(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			length, err = strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return nil, fmt.Errorf("lsp: unreadable Content-Length: %w", err)
			}
		}
	}
	if length < 0 {
		return nil, errors.New("lsp: message without Content-Length")
	}

	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// write frames and sends one message.
func (c *Client) write(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n%s", len(data), data)
	return err
}

// call sends a request and waits for the answer with its id, or for ctx.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := atomic.AddInt32(&c.reqID, 1)
	ch := make(chan response, 1)

	c.mu.Lock()
	if c.dead != nil {
		err := c.dead
		c.mu.Unlock()
		return nil, err
	}
	c.pending[id] = ch
	c.mu.Unlock()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	if err := c.write(req); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case res := <-ch:
		return res.result, res.err
	case <-ctx.Done():
		// Stop routing an answer nobody is waiting for any more.
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("lsp: %s: %w", method, ctx.Err())
	}
}

// notify sends a message that has no answer.
func (c *Client) notify(method string, params any) error {
	return c.write(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
}

// Close ends the conversation and waits for the process.
func (c *Client) Close() error {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd == nil {
		return nil
	}
	return c.cmd.Wait()
}

// Initialize performs the handshake. The server may refuse every request
// until it has been told the client is ready, so the notification that closes
// the handshake is part of it.
func (c *Client) Initialize(ctx context.Context, workspace string) error {
	params := map[string]any{
		"processId":    nil,
		"rootUri":      DocumentURI(workspace),
		"capabilities": map[string]any{},
	}
	if _, err := c.call(ctx, "initialize", params); err != nil {
		return err
	}
	return c.notify("initialized", map[string]any{})
}

// Definition reports where the symbol at a position is defined.
func (c *Client) Definition(ctx context.Context, path string, line, char int) (string, error) {
	res, err := c.call(ctx, "textDocument/definition", positionParams(path, line, char))
	if err != nil {
		return "", err
	}
	return string(res), nil
}

// References reports where the symbol at a position is used.
func (c *Client) References(ctx context.Context, path string, line, char int) (string, error) {
	params := positionParams(path, line, char)
	params["context"] = map[string]any{"includeDeclaration": true}
	res, err := c.call(ctx, "textDocument/references", params)
	if err != nil {
		return "", err
	}
	return string(res), nil
}

func positionParams(path string, line, char int) map[string]any {
	return map[string]any{
		"textDocument": map[string]any{"uri": DocumentURI(path)},
		"position":     map[string]any{"line": line, "character": char},
	}
}

// DocumentURI turns a filesystem path into the file URI the protocol expects.
// Concatenating "file://" is not the same thing: a space or an accent in a
// path has to be percent-encoded or the server cannot match the document.
func DocumentURI(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}
