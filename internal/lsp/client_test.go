package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
)

// frame wraps a payload in the LSP header framing.
func frame(payload string) string {
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(payload), payload)
}

// readFrame reads one framed message, the way a real language server would.
func readFrame(r *bufio.Reader) (map[string]any, error) {
	length := 0
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
				return nil, err
			}
		}
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	var msg map[string]any
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// fakeServer wires a Client to a scripted language server over pipes. The
// double cuts at the process boundary - the only external dependency here is
// the subprocess - and nowhere else.
func fakeServer(t *testing.T, handle func(msg map[string]any, w io.Writer)) *Client {
	t.Helper()

	clientReads, serverWrites := io.Pipe()
	serverReads, clientWrites := io.Pipe()

	c := newClient(clientWrites, clientReads)
	t.Cleanup(func() { _ = clientWrites.Close(); _ = serverWrites.Close() })

	go func() {
		r := bufio.NewReader(serverReads)
		for {
			msg, err := readFrame(r)
			if err != nil {
				return
			}
			handle(msg, serverWrites)
		}
	}()

	return c
}

// A real server interleaves notifications with responses. Taking the first
// message off the wire as the answer returns someone else's mail.
func TestCallSkipsNotificationsAndMatchesTheRequestID(t *testing.T) {
	c := fakeServer(t, func(msg map[string]any, w io.Writer) {
		id := msg["id"]
		fmt.Fprint(w, frame(`{"jsonrpc":"2.0","method":"window/logMessage","params":{"type":3,"message":"indexing"}}`))
		fmt.Fprint(w, frame(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":"THE-ANSWER"}`, id)))
	})

	got, err := c.Definition(context.Background(), "/ws/main.go", 3, 7)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if !strings.Contains(got, "THE-ANSWER") {
		t.Fatalf("got %q, want the response matching the request id", got)
	}
}

// Anything the reader buffered past one message belongs to the next one. A
// reader built per call throws that tail away, and the following call starts
// mid-message.
func TestSecondCallSurvivesABufferedTrailingNotification(t *testing.T) {
	noise := strings.Repeat("x", 8192)
	c := fakeServer(t, func(msg map[string]any, w io.Writer) {
		id := msg["id"]
		fmt.Fprint(w, frame(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":"answer-%v"}`, id, id)))
		fmt.Fprint(w, frame(fmt.Sprintf(`{"jsonrpc":"2.0","method":"$/progress","params":{"noise":%q}}`, noise)))
	})

	if _, err := c.Definition(context.Background(), "/ws/main.go", 1, 1); err != nil {
		t.Fatalf("first call: %v", err)
	}

	got, err := c.References(context.Background(), "/ws/main.go", 2, 2)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !strings.Contains(got, "answer-2") {
		t.Fatalf("got %q, want the second response", got)
	}
}

// A server that never answers must not hang the phase.
func TestCallReturnsWhenTheContextIsDone(t *testing.T) {
	c := fakeServer(t, func(msg map[string]any, w io.Writer) {
		// Deliberately mute.
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := c.Definition(ctx, "/ws/main.go", 1, 1)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("want an error when the context expires, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Definition ignored the context and hung")
	}
}

// An error from the server is the server's error, not a nil result.
func TestCallReportsTheServerError(t *testing.T) {
	c := fakeServer(t, func(msg map[string]any, w io.Writer) {
		id := msg["id"]
		fmt.Fprint(w, frame(fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%v,"error":{"code":-32601,"message":"no file for /ws/main.go"}}`, id)))
	})

	_, err := c.Definition(context.Background(), "/ws/main.go", 1, 1)
	if err == nil || !strings.Contains(err.Error(), "no file for") {
		t.Fatalf("got %v, want the server's error message", err)
	}
}

// A path is not a URI. Spaces and other characters have to survive the trip.
func TestDocumentURIIsEncoded(t *testing.T) {
	seen := make(chan string, 1)
	c := fakeServer(t, func(msg map[string]any, w io.Writer) {
		params, _ := msg["params"].(map[string]any)
		doc, _ := params["textDocument"].(map[string]any)
		uri, _ := doc["uri"].(string)
		seen <- uri
		fmt.Fprint(w, frame(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":[]}`, msg["id"])))
	})

	if _, err := c.Definition(context.Background(), "/ws/my project/main.go", 1, 1); err != nil {
		t.Fatalf("Definition: %v", err)
	}

	got := <-seen
	if got != "file:///ws/my%20project/main.go" {
		t.Fatalf("got %q, want a percent-encoded file URI", got)
	}
}

// The handshake is not over when initialize returns: a server may refuse
// every request until the client says it is ready.
func TestInitializeSendsTheInitializedNotification(t *testing.T) {
	methods := make(chan string, 4)
	c := fakeServer(t, func(msg map[string]any, w io.Writer) {
		method, _ := msg["method"].(string)
		methods <- method
		if id, ok := msg["id"]; ok {
			fmt.Fprint(w, frame(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":{}}`, id)))
		}
	})

	if err := c.Initialize(context.Background(), "/ws"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	if got := <-methods; got != "initialize" {
		t.Fatalf("first message was %q, want initialize", got)
	}
	select {
	case got := <-methods:
		if got != "initialized" {
			t.Fatalf("second message was %q, want the initialized notification", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the initialized notification was never sent")
	}
}
