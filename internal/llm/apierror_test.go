package llm

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestExtractsProviderMessageFromEnvelope(t *testing.T) {
	// The body below is the shape Gemini actually returned when the model in
	// GEMINI_MODEL had no free-tier quota. Before this, the whole envelope -
	// quota metric names, help links, retry hints, sixty lines of it - was
	// formatted into the error and printed to the terminal.
	body := []byte(`{
	  "error": {
	    "code": 429,
	    "message": "You exceeded your current quota, please check your plan and billing details.",
	    "status": "RESOURCE_EXHAUSTED",
	    "details": [{"@type": "type.googleapis.com/google.rpc.QuotaFailure"}]
	  }
	}`)

	err := NewAPIError("gemini", 429, body, nil)

	if !strings.Contains(err.Error(), "You exceeded your current quota") {
		t.Errorf("message not surfaced: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "RESOURCE_EXHAUSTED") {
		t.Errorf("status not surfaced: %q", err.Error())
	}
	if strings.Contains(err.Error(), "googleapis.com/google.rpc") {
		t.Errorf("raw envelope leaked into the message: %q", err.Error())
	}
}

func TestFallsBackToBodyWhenNotJSON(t *testing.T) {
	// A proxy or gateway answers with HTML, not the provider envelope.
	err := NewAPIError("openai", 502, []byte("<html><body>Bad Gateway</body></html>"), nil)

	if !strings.Contains(err.Error(), "Bad Gateway") {
		t.Errorf("body not surfaced: %q", err.Error())
	}
}

func TestTruncatesLongUnparseableBody(t *testing.T) {
	err := NewAPIError("openai", 500, []byte(strings.Repeat("x", 5000)), nil)

	if len(err.Error()) > maxBodyInError+200 {
		t.Errorf("error length %d: an unparseable body should be truncated", len(err.Error()))
	}
	if !strings.HasSuffix(err.Error(), "...") {
		t.Errorf("truncation not marked: %q", err.Error()[len(err.Error())-20:])
	}
}

func TestEmptyBodyStillNamesTheStatus(t *testing.T) {
	err := NewAPIError("gemini", 503, nil, nil)

	if !strings.Contains(err.Error(), "Service Unavailable") {
		t.Errorf("want the status text when there is no body, got %q", err.Error())
	}
}

func TestTransientClassification(t *testing.T) {
	cases := []struct {
		status int
		want   bool
		why    string
	}{
		{429, true, "a free tier queues rather than refuses"},
		{503, true, "measured on this project: 3.6 through 3.8-flash answered 503"},
		{500, true, "a gateway failing says nothing about the request"},
		{400, false, "a malformed body fails the same way every time"},
		{401, false, "a bad key fails the same way every time"},
		{404, false, "measured: a retired model answers 404, retrying cannot help"},
	}

	for _, c := range cases {
		got := (&APIError{Status: c.status}).Transient()
		if got != c.want {
			t.Errorf("status %d: Transient() = %v, want %v (%s)", c.status, got, c.want, c.why)
		}
	}
}

func TestRetryableIgnoresUnrelatedErrors(t *testing.T) {
	if Retryable(nil) {
		t.Error("nil must not be retryable")
	}
	if Retryable(errNotNetwork{}) {
		t.Error("an error that is neither an API status nor a timeout must not be retried")
	}
}

type errNotNetwork struct{}

func (errNotNetwork) Error() string { return "something else entirely" }

func TestReadsRetryDelayFromGoogleRetryInfo(t *testing.T) {
	// This is the body Gemini returned when the free tier ran out during a
	// real run. The provider states the wait; guessing a shorter one spends
	// another request against a window that is still closed.
	body := []byte(`{
	  "error": {
	    "code": 429,
	    "message": "You exceeded your current quota.",
	    "status": "RESOURCE_EXHAUSTED",
	    "details": [
	      {"@type": "type.googleapis.com/google.rpc.QuotaFailure"},
	      {"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "20.338440444s"}
	    ]
	  }
	}`)

	err := NewAPIError("gemini", 429, body, nil)

	if err.RetryAfter < 20*time.Second || err.RetryAfter > 21*time.Second {
		t.Errorf("RetryAfter = %v, want about 20.3s", err.RetryAfter)
	}
}

func TestReadsRetryAfterHeaderInSeconds(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "30")

	err := NewAPIError("openai", 429, nil, h)

	if err.RetryAfter != 30*time.Second {
		t.Errorf("RetryAfter = %v, want 30s", err.RetryAfter)
	}
}

func TestHeaderWinsOverBodyDelay(t *testing.T) {
	// A gateway in front of the provider knows when IT will accept traffic
	// again, and that is the hop this client actually talks to.
	body := []byte(`{"error":{"details":[
	  {"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"5s"}]}}`)
	h := http.Header{}
	h.Set("Retry-After", "45")

	err := NewAPIError("gemini", 429, body, h)

	if err.RetryAfter != 45*time.Second {
		t.Errorf("RetryAfter = %v, want the header's 45s", err.RetryAfter)
	}
}

func TestNoDelayWhenProviderSaysNothing(t *testing.T) {
	// Zero must mean "it did not say", never "retry at once".
	err := NewAPIError("gemini", 503, []byte(`{"error":{"message":"overloaded"}}`), nil)

	if err.RetryAfter != 0 {
		t.Errorf("RetryAfter = %v, want 0 when the provider gave no hint", err.RetryAfter)
	}
}

func TestIgnoresUnparseableRetryAfter(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "en un rato")

	if err := NewAPIError("openai", 429, nil, h); err.RetryAfter != 0 {
		t.Errorf("RetryAfter = %v, want 0 for an unparseable header", err.RetryAfter)
	}
}
