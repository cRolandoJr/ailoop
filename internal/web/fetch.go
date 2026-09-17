// Package web retrieves pages from permitted destinations.
//
// Nothing here has access to the workspace, and that is the point: the actor
// that can reach the network must not be the actor that can read local data.
// See AI_LOOP 18.15.7.
package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrNotAllowed means the destination is not on the allowlist.
	ErrNotAllowed = errors.New("destination not allowed")
	// ErrInternalTarget means the URL points inside the local network.
	ErrInternalTarget = errors.New("refusing to reach an internal address")
)

const (
	defaultMaxBytes = 400_000
	defaultTimeout  = 20 * time.Second
)

// Fetcher retrieves pages, read-only.
//
// It reaches anywhere on the public internet. A destination allowlist was
// considered and dropped: it does not defend against the real channel - what
// leaves is the question, not the destination - and it makes research useless,
// because you cannot know in advance where an answer lives.
//
// What it does NOT reach is the local network, and that limit stays no matter
// what anyone configures. Reaching a router, a NAS or a cloud metadata
// endpoint is not exfiltration, it is server-side request forgery: a different
// risk, which the research agent's isolation does not cover, because the agent
// runs INSIDE the perimeter even though it holds nothing from the project.
type Fetcher struct {
	// Allowed optionally restricts destinations. Empty means anywhere public,
	// which is the normal setting. A host covers its subdomains.
	Allowed []string
	// Blocked are hosts never fetched, checked before Allowed.
	Blocked []string
	// MaxBytes caps one response.
	MaxBytes int
	Timeout  time.Duration
	HTTP     *http.Client
}

// Page is what came back.
type Page struct {
	URL       string
	Text      string
	Truncated bool
}

func (f *Fetcher) maxBytes() int {
	if f.MaxBytes > 0 {
		return f.MaxBytes
	}
	return defaultMaxBytes
}

func (f *Fetcher) client() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	t := f.Timeout
	if t == 0 {
		t = defaultTimeout
	}
	return &http.Client{
		Timeout: t,
		// Redirects are re-checked: a permitted host that redirects to an
		// arbitrary one would walk straight around the allowlist.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return f.check(req.URL)
		},
	}
}

// Allows reports whether a URL may be fetched, and why not when it may not.
func (f *Fetcher) Allows(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("not a URL: %w", err)
	}
	return f.check(u)
}

func (f *Fetcher) check(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: only http and https are fetched, got %q", ErrNotAllowed, u.Scheme)
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("%w: no host", ErrNotAllowed)
	}

	// An internal address is refused even if somebody allowlisted it: reaching
	// localhost or a cloud metadata endpoint is not "fetching documentation",
	// it is server-side request forgery with extra steps.
	if isInternal(host) {
		return fmt.Errorf("%w: %s", ErrInternalTarget, host)
	}

	if matchesHost(host, f.Blocked) {
		return fmt.Errorf("%w: %s is on the blocklist", ErrNotAllowed, host)
	}

	// No allowlist means anywhere public: the agent looks things up wherever
	// the answer happens to be.
	if len(f.Allowed) == 0 {
		return nil
	}
	if matchesHost(host, f.Allowed) {
		return nil
	}
	return fmt.Errorf("%w: %s. This project restricts research to: %s",
		ErrNotAllowed, host, strings.Join(f.Allowed, ", "))
}

// matchesHost reports whether host is in list, where an entry covers its
// subdomains but never a name that merely ends the same way.
func matchesHost(host string, list []string) bool {
	for _, e := range list {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if host == e || strings.HasSuffix(host, "."+e) {
			return true
		}
	}
	return false
}

// isInternal reports whether a host resolves to somewhere inside the machine
// or the local network.
func isInternal(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return privateIP(ip)
	}
	// A name can still resolve inward. Resolving it here costs one lookup and
	// closes the most common bypass.
	addrs, err := net.LookupIP(host)
	if err != nil {
		return false // unresolvable: the request will fail on its own
	}
	for _, ip := range addrs {
		if privateIP(ip) {
			return true
		}
	}
	return false
}

func privateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	// 169.254.169.254 and friends are link-local, already covered, but the
	// IPv6 unique-local range is not.
	return len(ip) == net.IPv6len && ip[0]&0xfe == 0xfc
}

// Fetch retrieves one page. It is read-only by construction: there is no code
// path here that sends a body.
func (f *Fetcher) Fetch(ctx context.Context, raw string) (*Page, error) {
	raw = strings.TrimSpace(raw)
	if err := f.Allows(raw); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ailoop/0.1 (research agent)")
	req.Header.Set("Accept", "text/html,text/plain,application/json;q=0.9,*/*;q=0.5")

	resp, err := f.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %d", raw, resp.StatusCode)
	}

	limit := f.maxBytes()
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return nil, err
	}

	p := &Page{URL: raw}
	if len(body) > limit {
		body = body[:limit]
		p.Truncated = true
	}

	text := string(body)
	if strings.Contains(resp.Header.Get("Content-Type"), "html") {
		text = StripHTML(text)
	}
	p.Text = text
	return p, nil
}

// fetchNoCheck performs the request without the allowlist gate. It exists for
// tests of the parsing path against a local test server, which the gate
// correctly refuses.
func (f *Fetcher) fetchNoCheck(ctx context.Context, raw string) (*Page, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(f.maxBytes())))
	if err != nil {
		return nil, err
	}
	text := string(body)
	if strings.Contains(resp.Header.Get("Content-Type"), "html") {
		text = StripHTML(text)
	}
	return &Page{URL: raw, Text: text}, nil
}
