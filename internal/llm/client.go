package llm

import "context"

// Image is visual input attached to a message. Providers that cannot see it
// say so through Capabilities rather than silently dropping it.
type Image struct {
	// MediaType is the IANA type, e.g. "image/png".
	MediaType string
	// Data is the raw bytes, not base64: each adapter encodes as its wire
	// format needs.
	Data []byte
}

// Message represents a chat message.
type Message struct {
	Role    string // "user", "assistant", "system"
	Content string
	// Images is optional visual input. Only meaningful when the provider
	// reports Vision as supported.
	Images []Image
	// Round is which tool-loop round produced this message, used to decide
	// what is old enough to elide. Zero is a real round, so anything outside
	// a loop sets -1.
	Round int `json:"-"`
}

// Support is the availability of a capability, with the four states of
// AI_LOOP 18.16. Unknown is a first-class answer: claiming a capability we
// have not established is the same mistake as an agent claiming a fact it
// has not checked.
type Support int

const (
	// Unknown means availability has not been established reliably.
	Unknown Support = iota
	// Supported means the provider exposes it and we can use it.
	Supported
	// Unsupported means the provider does not expose it.
	Unsupported
	// Blocked means it exists but a prerequisite or permission prevents use.
	Blocked
)

func (s Support) String() string {
	switch s {
	case Supported:
		return "available"
	case Unsupported:
		return "unavailable"
	case Blocked:
		return "blocked"
	default:
		return "unknown"
	}
}

// OK reports whether the capability may actually be used. Unknown is not OK:
// fail-closed, so an unverified capability is never offered to an agent.
func (s Support) OK() bool { return s == Supported }

// Capabilities describes what the selected provider and model can technically
// do. It is descriptive only: it grants nothing. Permission is decided
// elsewhere, per phase.
type Capabilities struct {
	Provider string
	Model    string

	// Vision is whether the model can read images.
	Vision Support
	// Thinking is whether the model exposes extended reasoning.
	Thinking Support
	// MaxContextTokens is 0 when unknown.
	MaxContextTokens int
	// PromptCaching is whether a stable prompt prefix is billed at a discount.
	// Some providers do it automatically, some need an explicit marker, and
	// some do not do it at all - the workflow only needs to know whether
	// designing for prefix stability pays here.
	PromptCaching Support
}

// Client defines the interface for interacting with any LLM.
type Client interface {
	// Generate takes a conversation history and returns the response together
	// with what it cost. Every provider reports cost differently and some not
	// at all; each adapter normalises into Usage and marks estimates.
	Generate(ctx context.Context, messages []Message) (Response, error)
	// Describe reports what this provider and model can do. It is how the
	// workflow stops hardcoding capabilities per provider.
	Describe() Capabilities
}
