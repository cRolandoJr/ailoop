package attach

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cRolandoJr/ailoop/internal/llm"
)

const (
	// MaxImageBytes caps one image. Images are expensive in tokens and a
	// screenshot of a 4K display is large.
	MaxImageBytes = 5 << 20
	toolTimeout   = 30 * time.Second
)

var imageTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp",
}

func isImageExt(ext string) bool { _, ok := imageTypes[ext]; return ok }

func (r *Resolver) image(ref, full string) (Attachment, error) {
	info, err := os.Stat(full)
	if err != nil {
		return Attachment{}, err
	}
	if info.Size() > MaxImageBytes {
		return Attachment{}, fmt.Errorf("image is %d bytes, over the %d limit", info.Size(), MaxImageBytes)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return Attachment{}, err
	}
	return Attachment{
		Ref: ref, Kind: KindImage, Source: full,
		Image: llm.Image{MediaType: imageTypes[strings.ToLower(filepath.Ext(full))], Data: data},
	}, nil
}

// pdf extracts text with pdftotext.
//
// -layout keeps columns and indentation: without it a two-column paper comes
// out interleaved and unreadable. Measured on real course material in a sister
// project, 60 of 61 files converted.
func (r *Resolver) pdf(ref, full string) (Attachment, error) {
	if !r.Host.Has("pdftotext") {
		return Attachment{}, fmt.Errorf(
			"reading PDFs needs pdftotext (package poppler-utils). Run 'ailoop doctor'")
	}

	out, err := run(context.Background(), "pdftotext", "-layout", "-enc", "UTF-8", full, "-")
	if err != nil {
		return Attachment{}, err
	}

	// "It converted" is not exit 0, it is "text came out": a scanned PDF with
	// no text layer exits 0 and returns page breaks.
	if len(strings.Fields(out)) < 20 {
		return Attachment{}, fmt.Errorf(
			"almost no text came out: it is probably a scan with no text layer")
	}
	if len(out) > MaxTextBytes {
		out = out[:MaxTextBytes] + "\n... (truncated)"
	}
	return Attachment{Ref: ref, Kind: KindText, Source: full, Text: out}, nil
}

// screen captures the display, or a region of it when select is true.
//
// This is only ever reached because a person typed @screen. It is not a
// capability and no agent can ask for it: a screenshot holds whatever happens
// to be open, which is not the agent's to decide to look at.
func (r *Resolver) screen(ref string, selectRegion bool) (Attachment, error) {
	if !r.Host.Has("grim") {
		return Attachment{}, fmt.Errorf(
			"capturing the screen needs grim (Wayland). Run 'ailoop doctor'")
	}
	if selectRegion && !r.Host.Has("slurp") {
		return Attachment{}, fmt.Errorf(
			"selecting a region needs slurp. Use @screen for the whole display")
	}

	tmp, err := os.CreateTemp("", "ailoop-screen-*.png")
	if err != nil {
		return Attachment{}, err
	}
	path := tmp.Name()
	tmp.Close()
	// The capture is not left lying around: it may hold anything that was on
	// screen.
	defer os.Remove(path)

	args := []string{}
	if selectRegion {
		region, err := run(context.Background(), "slurp")
		if err != nil {
			return Attachment{}, fmt.Errorf("no region selected: %w", err)
		}
		args = append(args, "-g", strings.TrimSpace(region))
	}
	args = append(args, path)

	if _, err := run(context.Background(), "grim", args...); err != nil {
		return Attachment{}, err
	}

	att, err := r.image(ref, path)
	if err != nil {
		return Attachment{}, err
	}
	att.Source = "screen capture"
	return att, nil
}

// clipboard reads whatever the clipboard holds, text or image.
func (r *Resolver) clipboard(ref string) (Attachment, error) {
	if !r.Host.Has("wl-paste") {
		return Attachment{}, fmt.Errorf(
			"reading the clipboard needs wl-paste (package wl-clipboard). Run 'ailoop doctor'")
	}

	types, err := run(context.Background(), "wl-paste", "--list-types")
	if err != nil {
		return Attachment{}, fmt.Errorf("the clipboard is empty or unreadable: %w", err)
	}

	for ext, mime := range imageTypes {
		if !strings.Contains(types, mime) {
			continue
		}
		data, err := runBytes(context.Background(), "wl-paste", "--type", mime)
		if err != nil {
			return Attachment{}, err
		}
		if len(data) > MaxImageBytes {
			return Attachment{}, fmt.Errorf("the clipboard image is over the %d byte limit", MaxImageBytes)
		}
		_ = ext
		return Attachment{
			Ref: ref, Kind: KindImage, Source: "clipboard",
			Image: llm.Image{MediaType: mime, Data: data},
		}, nil
	}

	text, err := run(context.Background(), "wl-paste", "--no-newline")
	if err != nil {
		return Attachment{}, err
	}
	if strings.TrimSpace(text) == "" {
		return Attachment{}, fmt.Errorf("the clipboard is empty")
	}
	if len(text) > MaxTextBytes {
		text = text[:MaxTextBytes] + "\n... (truncated)"
	}
	return Attachment{Ref: ref, Kind: KindText, Source: "clipboard", Text: text}, nil
}

func run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := runBytes(ctx, name, args...)
	return string(out), err
}

func runBytes(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		// stderr is kept: suppressing it turns "command not found" into a
		// mysterious empty result.
		return nil, fmt.Errorf("%s: %s", name, msg)
	}
	return stdout.Bytes(), nil
}
