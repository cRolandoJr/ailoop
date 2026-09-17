// Package attach resolves the @references a person writes into things the
// model can read.
//
// Everything here is the HUMAN's channel, and that is why it may reach outside
// the workspace. Writing "@~/Downloads/spec.pdf" IS the authorisation, given
// case by case. The agent's own channel - fs.read, fs.grep - stays confined to
// the workspace, because nobody authorised anything there: it is helping
// itself.
//
// The same distinction is why @screen lives here and is never a capability.
// An agent that could capture the screen whenever it wanted would see the
// password manager, the mail, whatever happens to be open.
package attach

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cRolandoJr/ailoop/internal/host"
	"github.com/cRolandoJr/ailoop/internal/llm"
)

// Kind is what a reference resolved to.
type Kind int

const (
	KindText Kind = iota
	KindImage
)

// Attachment is one resolved reference.
type Attachment struct {
	// Ref is what the person wrote, e.g. "@main.go".
	Ref  string
	Kind Kind
	// Source says where it came from, and is shown to the model so it can
	// cite it.
	Source string
	Text   string
	Image  llm.Image
}

// Resolver turns references into attachments, using what this machine offers.
type Resolver struct {
	Workspace string
	Host      host.Report
}

// New builds a resolver for a workspace.
func New(workspace string) *Resolver {
	return &Resolver{Workspace: workspace, Host: host.Discover()}
}

// Expand finds every @reference in the input, resolves what it can, and
// returns the text with the references left in place.
//
// The references stay in the text on purpose: the person wrote "look at
// @main.go and tell me", and the sentence has to keep making sense. The
// attachment is what carries the contents.
func (r *Resolver) Expand(input string) (atts []Attachment, problems []error) {
	seen := map[string]bool{}

	for _, ref := range FindRefs(input) {
		if seen[ref] {
			continue
		}
		seen[ref] = true

		att, err := r.resolve(ref)
		if err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", ref, err))
			continue
		}
		atts = append(atts, att)
	}
	return atts, problems
}

// FindRefs extracts the references from a line of text.
//
// A reference starts at a word boundary, which keeps "user@example.com" from
// being read as one.
func FindRefs(input string) []string {
	var refs []string
	for i := 0; i < len(input); i++ {
		if input[i] != '@' {
			continue
		}
		if i > 0 && !isBoundary(input[i-1]) {
			continue // part of a word: an address, a handle
		}
		j := i + 1
		for j < len(input) && !isSpace(input[j]) {
			j++
		}
		// Trailing punctuation belongs to the sentence, not to the path.
		end := j
		for end > i+1 && strings.ContainsRune(".,;:!?)]}", rune(input[end-1])) {
			end--
		}
		if end > i+1 {
			refs = append(refs, input[i:end])
		}
		i = j
	}
	return refs
}

func isBoundary(c byte) bool {
	return isSpace(c) || strings.ContainsRune("([{\"'", rune(c))
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// resolve turns one reference into an attachment.
func (r *Resolver) resolve(ref string) (Attachment, error) {
	body := strings.TrimPrefix(ref, "@")

	switch {
	case body == "screen":
		return r.screen(ref, false)
	case body == "screen:select":
		return r.screen(ref, true)
	case body == "clipboard":
		return r.clipboard(ref)
	default:
		return r.file(ref, body)
	}
}

// MaxTextBytes caps one text attachment. It travels in every request of every
// round, so a whole book pasted by accident is expensive for a long time.
const MaxTextBytes = 100_000

func (r *Resolver) file(ref, path string) (Attachment, error) {
	full, err := r.expandPath(path)
	if err != nil {
		return Attachment{}, err
	}

	info, err := os.Stat(full)
	if err != nil {
		return Attachment{}, fmt.Errorf("cannot read it: %w", err)
	}
	if info.IsDir() {
		return Attachment{}, fmt.Errorf("it is a directory")
	}

	ext := strings.ToLower(filepath.Ext(full))
	switch {
	case ext == ".pdf":
		return r.pdf(ref, full)
	case isImageExt(ext):
		return r.image(ref, full)
	default:
		return r.text(ref, full)
	}
}

// expandPath resolves ~ and relative paths. It does NOT confine the result to
// the workspace: this is the human's channel.
func (r *Resolver) expandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	if filepath.IsAbs(path) {
		return path, nil
	}
	return filepath.Join(r.Workspace, path), nil
}

func (r *Resolver) text(ref, full string) (Attachment, error) {
	data, err := os.ReadFile(full)
	if err != nil {
		return Attachment{}, err
	}
	body := string(data)
	if len(body) > MaxTextBytes {
		body = body[:MaxTextBytes] + "\n... (truncated)"
	}
	return Attachment{Ref: ref, Kind: KindText, Source: full, Text: body}, nil
}

// Render turns attachments into the text block that goes to the model.
func Render(atts []Attachment) string {
	var b strings.Builder
	for _, a := range atts {
		if a.Kind != KindText {
			fmt.Fprintf(&b, "\n<ATTACHMENT ref=%q source=%q type=\"image\"/>\n", a.Ref, a.Source)
			continue
		}
		fmt.Fprintf(&b, "\n<ATTACHMENT ref=%q source=%q>\n%s\n</ATTACHMENT>\n", a.Ref, a.Source, a.Text)
	}
	return b.String()
}

// Images returns the visual attachments, for a model that can see them.
func Images(atts []Attachment) []llm.Image {
	var out []llm.Image
	for _, a := range atts {
		if a.Kind == KindImage {
			out = append(out, a.Image)
		}
	}
	return out
}

// Volatile reports whether a reference points at something that will not be
// there later: the screen changes, the clipboard changes.
func Volatile(ref string) bool {
	body := strings.TrimPrefix(ref, "@")
	return body == "screen" || body == "screen:select" || body == "clipboard"
}

// Freeze resolves the volatile references in a text, writes them to files
// under dir, and rewrites the text to point at those files.
//
// It exists because of when rejection feedback is read. A person writes "this
// is wrong, look at @screen" and the turn ends; the agent sees that sentence
// on the NEXT run, minutes later, by which time capturing the screen would
// photograph something else entirely. Freezing turns the moment into a file,
// and the file resolves like any other afterwards.
//
// Errors are returned, not swallowed: a reference that could not be frozen is
// left in the text as the person wrote it, so nothing silently disappears.
func (r *Resolver) Freeze(dir, text string) (string, []error) {
	var problems []error

	for _, ref := range FindRefs(text) {
		if !Volatile(ref) {
			continue
		}

		att, err := r.resolve(ref)
		if err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", ref, err))
			continue
		}

		saved, err := r.save(dir, ref, att)
		if err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", ref, err))
			continue
		}
		text = strings.ReplaceAll(text, ref, "@"+saved)
	}

	return text, problems
}

// save writes an attachment next to the work's state and returns its path.
func (r *Resolver) save(dir, ref string, att Attachment) (string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	base := strings.NewReplacer("@", "", ":", "-").Replace(ref)
	stamp := time.Now().Format("20060102-150405")

	var name string
	var data []byte
	if att.Kind == KindImage {
		name = fmt.Sprintf("%s-%s%s", base, stamp, extFor(att.Image.MediaType))
		data = att.Image.Data
	} else {
		name = fmt.Sprintf("%s-%s.txt", base, stamp)
		data = []byte(att.Text)
	}

	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, data, 0644); err != nil {
		return "", err
	}
	return full, nil
}

func extFor(mediaType string) string {
	for ext, mime := range imageTypes {
		if mime == mediaType {
			return ext
		}
	}
	return ".bin"
}
