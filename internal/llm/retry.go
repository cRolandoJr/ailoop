package llm

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

// DefaultAttempts is how many times a request is sent before giving up.
//
// Three is chosen against a measurement, not a habit: on this project's free
// Gemini tier one request in three returned its first byte after 68 seconds
// while the other two answered in under 10. Two attempts would still fail
// roughly one run in nine; a fourth buys little against a provider that is
// genuinely down.
const DefaultAttempts = 3

// maxServerWait bounds how long a provider may ask this client to sleep.
//
// A provider answering "retry in an hour" is not reporting a blip, it is
// reporting that the quota is gone. Sleeping through it would freeze the run
// with nothing on screen for an hour; failing instead surfaces the message,
// which already states the wait, and leaves the decision with the person. The
// measured case that set this bound asked for 20 seconds.
const maxServerWait = 90 * time.Second

// retryClient re-sends a request that failed for a reason that says nothing
// about the request itself.
//
// It is a decorator: it satisfies Client by embedding one, so Describe and any
// method added later pass through untouched. The alternative - putting retry
// logic inside each adapter - would have written the same loop three times and
// left the next adapter to remember it.
type retryClient struct {
	Client
	attempts int
	// wait is injected so tests do not sleep. A test that waits out a real
	// backoff either takes seconds or gets shortened until it proves nothing.
	wait func(ctx context.Context, attempt int, hint time.Duration) error
}

// WithRetry wraps a client so transient failures are re-sent.
//
// It does not wrap the Claude adapter's own retries: the official SDK already
// classifies and retries, and stacking two policies multiplies the wait
// without improving the odds.
func WithRetry(c Client, attempts int) Client {
	if attempts < 1 {
		attempts = DefaultAttempts
	}
	return &retryClient{Client: c, attempts: attempts, wait: backoff}
}

// backoff sleeps longer after each failure, with jitter.
//
// The jitter matters when several loops share a quota: without it, every
// client that got the same 429 comes back at the same instant and reproduces
// the overload it is backing off from - a thundering herd.
// backoffDelay picks how long to wait. Split from the sleeping so the choice
// can be asserted without a test that actually waits.
func backoffDelay(attempt int, hint time.Duration) time.Duration {
	base := time.Duration(1<<uint(attempt)) * time.Second
	d := base + time.Duration(rand.Int63n(int64(base/2)))

	// A provider that stated the wait knows when its window reopens; a shorter
	// local guess just spends another request against a window still closed,
	// which on a quota error is the opposite of backing off.
	if hint > d {
		d = hint + time.Duration(rand.Int63n(int64(time.Second)))
	}
	return d
}

func backoff(ctx context.Context, attempt int, hint time.Duration) error {
	t := time.NewTimer(backoffDelay(attempt, hint))
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (r *retryClient) Generate(ctx context.Context, messages []Message) (Response, error) {
	var resp Response
	var err error
	for attempt := 1; ; attempt++ {
		resp, err = r.Client.Generate(ctx, messages)
		if !r.shouldRetry(err, attempt) {
			return resp, err
		}
		if werr := r.wait(ctx, attempt, retryHint(err)); werr != nil {
			return resp, err // the original failure, not the interrupted sleep
		}
	}
}

// GenerateStream retries only a request that produced no output.
//
// Once a chunk has been printed, the person is reading a partial answer;
// re-sending would print a second answer under the first and there is no way
// to unprint the first. A stream that dies mid-answer is therefore surfaced
// as the failure it is, rather than retried into a corrupted transcript.
func (r *retryClient) GenerateStream(ctx context.Context, messages []Message, onChunk func(string)) (Response, error) {
	var resp Response
	var err error
	for attempt := 1; ; attempt++ {
		emitted := false
		resp, err = r.Client.GenerateStream(ctx, messages, func(chunk string) {
			emitted = true
			onChunk(chunk)
		})
		if emitted || !r.shouldRetry(err, attempt) {
			return resp, err
		}
		if werr := r.wait(ctx, attempt, retryHint(err)); werr != nil {
			return resp, err
		}
	}
}

func (r *retryClient) shouldRetry(err error, attempt int) bool {
	if err == nil || attempt >= r.attempts || !Retryable(err) {
		return false
	}
	// Asking for longer than this client will sleep is the provider saying the
	// quota is spent, not that the request hiccuped.
	return retryHint(err) <= maxServerWait
}

// retryHint is the wait the provider asked for, or zero when it said nothing.
func retryHint(err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.RetryAfter
	}
	return 0
}
