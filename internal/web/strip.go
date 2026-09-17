package web

import "strings"

// StripHTML turns a page into readable text.
//
// Deliberately small and dependency-free. It is not a parser and does not try
// to be: what the research agent needs is prose, and an exact DOM buys nothing
// here while adding a dependency to a package that talks to the network.
func StripHTML(s string) string {
	// Whole elements whose contents are never prose.
	for _, tag := range []string{"script", "style", "noscript", "svg", "head"} {
		s = dropElement(s, tag)
	}

	var b strings.Builder
	b.Grow(len(s) / 2)

	inTag := false
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '<':
			inTag = true
			// Block-level tags become line breaks so the text keeps its shape.
			if isBlockBoundary(s[i:]) {
				b.WriteByte('\n')
			}
		case s[i] == '>':
			inTag = false
		case !inTag:
			b.WriteByte(s[i])
		}
	}

	return collapse(unescape(b.String()))
}

func dropElement(s, tag string) string {
	open, close := "<"+tag, "</"+tag
	for {
		i := indexFold(s, open)
		if i == -1 {
			return s
		}
		j := indexFold(s[i:], close)
		if j == -1 {
			return s[:i] // unterminated: drop the rest, it is not prose
		}
		end := i + j
		if k := strings.IndexByte(s[end:], '>'); k != -1 {
			end += k + 1
		}
		s = s[:i] + " " + s[end:]
	}
}

func indexFold(s, sub string) int {
	return strings.Index(strings.ToLower(s), strings.ToLower(sub))
}

var blockTags = []string{"<p", "</p", "<br", "<div", "</div", "<li", "</li",
	"<tr", "</tr", "<h1", "<h2", "<h3", "<h4", "</h1", "</h2", "</h3", "</h4",
	"<pre", "</pre", "<section", "</section", "<article", "</article"}

func isBlockBoundary(s string) bool {
	low := strings.ToLower(s)
	for _, t := range blockTags {
		if strings.HasPrefix(low, t) {
			return true
		}
	}
	return false
}

var entities = strings.NewReplacer(
	"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
	"&quot;", "\"", "&#39;", "'", "&apos;", "'", "&mdash;", "-", "&ndash;", "-",
)

func unescape(s string) string { return entities.Replace(s) }

// collapse removes the blank-line drifts that stripping tags leaves behind.
func collapse(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, l := range lines {
		l = strings.TrimRight(l, " \t\r")
		if strings.TrimSpace(l) == "" {
			blank++
			if blank > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blank = 0
		out = append(out, strings.TrimLeft(l, " \t"))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
