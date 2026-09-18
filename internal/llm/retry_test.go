package llm

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"
)

// fakeClient answers from a scripted list of results, one per call.
type fakeClient struct {
	errs   []error
	chunks []string // emitted by GenerateStream before the error, per call
	calls  int
}

func (f *fakeClient) Describe() Capabilities { return Capabilities{Provider: "fake"} }

func (f *fakeClient) Generate(context.Context, []Message) (Response, error) {
	i := f.calls
	f.calls++
	if i < len(f.errs) && f.errs[i] != nil {
		return Response{}, f.errs[i]
	}
	return Response{Text: "ok"}, nil
}

func (f *fakeClient) GenerateStream(_ context.Context, _ []Message, onChunk func(string)) (Response, error) {
	i := f.calls
	f.calls++
	if i < len(f.chunks) && f.chunks[i] != "" {
		onChunk(f.chunks[i])
	}
	if i < len(f.errs) && f.errs[i] != nil {
		return Response{}, f.errs[i]
	}
	return Response{Text: "ok"}, nil
}

// noWait replaces the backoff so the tests do not sleep.
func noWait(context.Context, int, time.Duration) error { return nil }

func wrap(f *fakeClient, attempts int) *retryClient {
	return &retryClient{Client: f, attempts: attempts, wait: noWait}
}

func TestRetriesTransientThenSucceeds(t *testing.T) {
	f := &fakeClient{errs: []error{
		&APIError{Provider: "gemini", Status: 503},
		nil,
	}}
	r := wrap(f, 3)

	resp, err := r.Generate(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected success after one retry, got %v", err)
	}
	if resp.Text != "ok" {
		t.Errorf("text = %q, want ok", resp.Text)
	}
	if f.calls != 2 {
		t.Errorf("calls = %d, want 2", f.calls)
	}
}

func TestDoesNotRetryPermanentError(t *testing.T) {
	// A bad key fails identically however often it is sent. Retrying it
	// would triple the time to a message the person needs immediately.
	f := &fakeClient{errs: []error{
		&APIError{Provider: "gemini", Status: 401, Message: "API key not valid"},
		nil,
	}}
	r := wrap(f, 3)

	if _, err := r.Generate(context.Background(), nil); err == nil {
		t.Fatal("expected the 401 to surface, not be retried away")
	}
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1: a 401 must not be retried", f.calls)
	}
}

func TestStopsAtAttemptCeiling(t *testing.T) {
	f := &fakeClient{errs: []error{
		&APIError{Status: 503}, &APIError{Status: 503}, &APIError{Status: 503}, nil,
	}}
	r := wrap(f, 3)

	if _, err := r.Generate(context.Background(), nil); err == nil {
		t.Fatal("expected failure once attempts ran out")
	}
	if f.calls != 3 {
		t.Errorf("calls = %d, want 3", f.calls)
	}
}

func TestRetriesNetworkTimeout(t *testing.T) {
	// This is the shape the failure actually arrived in: a *url.Error
	// wrapping a timeout, from http.Client giving up awaiting headers.
	f := &fakeClient{errs: []error{
		&url.Error{Op: "Post", URL: "https://example", Err: timeoutErr{}},
		nil,
	}}
	r := wrap(f, 3)

	if _, err := r.Generate(context.Background(), nil); err != nil {
		t.Fatalf("a network timeout should be retried, got %v", err)
	}
	if f.calls != 2 {
		t.Errorf("calls = %d, want 2", f.calls)
	}
}

func TestStreamNotRetriedOnceItHasEmitted(t *testing.T) {
	// The decision this pins: a stream that already printed part of an answer
	// is never re-sent, because the person is reading that text and a second
	// attempt would print a second answer underneath the first.
	f := &fakeClient{
		chunks: []string{"partial answer", ""},
		errs:   []error{&APIError{Status: 503}, nil},
	}
	r := wrap(f, 3)

	var got string
	_, err := r.GenerateStream(context.Background(), nil, func(c string) { got += c })
	if err == nil {
		t.Fatal("expected the failure to surface rather than be retried")
	}
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1: emitted output must not be retried", f.calls)
	}
	if got != "partial answer" {
		t.Errorf("chunks = %q, want the single partial answer", got)
	}
}

func TestStreamRetriedWhenNothingWasEmitted(t *testing.T) {
	f := &fakeClient{
		chunks: []string{"", "full answer"},
		errs:   []error{&APIError{Status: 503}, nil},
	}
	r := wrap(f, 3)

	var got string
	if _, err := r.GenerateStream(context.Background(), nil, func(c string) { got += c }); err != nil {
		t.Fatalf("expected the retry to succeed, got %v", err)
	}
	if f.calls != 2 {
		t.Errorf("calls = %d, want 2", f.calls)
	}
	if got != "full answer" {
		t.Errorf("chunks = %q, want only the successful attempt's text", got)
	}
}

func TestCancelledContextStopsRetrying(t *testing.T) {
	f := &fakeClient{errs: []error{&APIError{Status: 503}, nil}}
	r := &retryClient{Client: f, attempts: 3, wait: func(context.Context, int, time.Duration) error {
		return context.Canceled
	}}

	_, err := r.Generate(context.Background(), nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want the original API error to surface, got %v", err)
	}
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1", f.calls)
	}
}

// timeoutErr is a minimal net.Error reporting a timeout.
type timeoutErr struct{}

func (timeoutErr) Error() string { return "i/o timeout" }
func (timeoutErr) Timeout() bool { return true }
func (timeoutErr) Temporary() bool {
	return true
}

func TestTheProvidersDelayReachesTheBackoff(t *testing.T) {
	f := &fakeClient{errs: []error{
		&APIError{Status: 429, RetryAfter: 20 * time.Second},
		nil,
	}}
	var seen time.Duration
	r := &retryClient{Client: f, attempts: 3, wait: func(_ context.Context, _ int, hint time.Duration) error {
		seen = hint
		return nil
	}}

	if _, err := r.Generate(context.Background(), nil); err != nil {
		t.Fatalf("expected the retry to succeed, got %v", err)
	}
	if seen != 20*time.Second {
		t.Errorf("the backoff got hint %v, want the provider's 20s", seen)
	}
}

func TestDoesNotRetryWhenTheProviderAsksForTooLong(t *testing.T) {
	// A wait this long is the provider saying the quota is spent. Sleeping
	// through it freezes the run with nothing on screen; the error already
	// states the wait, so it goes to the person instead.
	f := &fakeClient{errs: []error{
		&APIError{Status: 429, RetryAfter: time.Hour, Message: "quota exhausted"},
		nil,
	}}
	r := wrap(f, 3)

	if _, err := r.Generate(context.Background(), nil); err == nil {
		t.Fatal("expected the quota error to surface")
	}
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1: an hour-long wait must not be slept through", f.calls)
	}
}

func TestBackoffNeverWaitsLessThanTheProviderAsked(t *testing.T) {
	// A local guess of ~2s must not win over a stated 20s, or every retry
	// lands on a window that is still closed.
	const hint = 20 * time.Second
	for attempt := 1; attempt <= 3; attempt++ {
		if d := backoffDelay(attempt, hint); d < hint {
			t.Errorf("attempt %d: delay %v is shorter than the requested %v", attempt, d, hint)
		}
	}
}

func TestBackoffGrowsWhenTheProviderSaidNothing(t *testing.T) {
	// With no hint the wait still has to grow, or three attempts land inside
	// the same overloaded instant.
	first := backoffDelay(1, 0)
	third := backoffDelay(3, 0)
	if third <= first {
		t.Errorf("delay did not grow: attempt 1 = %v, attempt 3 = %v", first, third)
	}
}
