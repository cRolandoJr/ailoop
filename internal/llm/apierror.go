package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxBodyInError bounds how much of an unparseable body reaches the terminal.
// A provider that answers with an HTML error page would otherwise print the
// whole page.
const maxBodyInError = 400

// APIError is a provider response that was not a success, with the status kept
// as data rather than baked into a string.
//
// It exists because two separate needs read the same response: the retry
// decorator has to tell a transient 503 from a permanent 400, and the person
// at the terminal needs the one sentence the provider actually wrote. Both
// were previously impossible - the adapters formatted the entire JSON body
// into an error string, so the status was unreachable to code and the message
// was buried in sixty lines of envelope.
type APIError struct {
	Provider string
	Status   int
	// Message is the provider's own explanation, when the body carried one.
	Message string
	// Body is the raw response, kept for the cases where nothing parsed.
	Body string
	// RetryAfter is how long the provider asked the caller to wait. Zero
	// means it did not say, not "retry immediately".
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	detail := e.Message
	if detail == "" {
		detail = strings.TrimSpace(e.Body)
		if len(detail) > maxBodyInError {
			detail = detail[:maxBodyInError] + "..."
		}
	}
	if detail == "" {
		detail = http.StatusText(e.Status)
	}
	return fmt.Sprintf("%s API error (status %d): %s", e.Provider, e.Status, detail)
}

// Transient reports whether retrying the same request could plausibly succeed.
//
// 429 and 503 are the two that matter in practice: a free tier queues rather
// than refuses. 5xx is included because a gateway failing is not a statement
// about the request. Everything else - a bad key, a model that does not
// exist, a malformed body - fails identically no matter how often it is sent.
func (e *APIError) Transient() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// errorEnvelope is the shape both Gemini and OpenAI-compatible providers use.
type errorEnvelope struct {
	Error struct {
		Message string `json:"message"`
		Status  string `json:"status"`
		// Details carries Google's RetryInfo, which states the wait in
		// seconds rather than leaving the client to guess one.
		Details []struct {
			Type       string `json:"@type"`
			RetryDelay string `json:"retryDelay"`
		} `json:"details"`
	} `json:"error"`
}

// NewAPIError builds the error for a non-200 response, pulling out the
// provider's message when the body carries the usual envelope.
func NewAPIError(provider string, status int, body []byte, header http.Header) *APIError {
	e := &APIError{Provider: provider, Status: status, Body: string(body)}

	var env errorEnvelope
	if err := json.Unmarshal(body, &env); err == nil {
		if msg := strings.TrimSpace(env.Error.Message); msg != "" {
			e.Message = msg
			if env.Error.Status != "" {
				e.Message += " (" + env.Error.Status + ")"
			}
		}
		for _, d := range env.Error.Details {
			if !strings.Contains(d.Type, "RetryInfo") || d.RetryDelay == "" {
				continue
			}
			if wait, perr := time.ParseDuration(d.RetryDelay); perr == nil && wait > 0 {
				e.RetryAfter = wait
			}
		}
	}

	// The HTTP header wins when both are present: it is the standard one, and
	// a proxy or gateway in front of the provider can set it when the body
	// says nothing.
	if wait, ok := parseRetryAfter(header); ok {
		e.RetryAfter = wait
	}
	return e
}

// parseRetryAfter reads the Retry-After header, which RFC 9110 allows to be
// either a number of seconds or an HTTP date.
func parseRetryAfter(header http.Header) (time.Duration, bool) {
	v := strings.TrimSpace(header.Get("Retry-After"))
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	if when, err := http.ParseTime(v); err == nil {
		if d := time.Until(when); d > 0 {
			return d, true
		}
	}
	return 0, false
}

// Retryable reports whether an error is worth sending again.
//
// It covers two distinct failures that look nothing alike: a provider that
// answered with a transient status, and a request that never got an answer.
// The second is the one measured on this project - Gemini's free tier
// returned a first byte after 68 seconds on one request in three, so a
// network timeout here says nothing about whether the request was valid.
func Retryable(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Transient()
	}

	// A timeout arrives wrapped in *url.Error; net.Error is what survives it.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}
