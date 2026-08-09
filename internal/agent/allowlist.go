package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/sl6117/automations/internal/ai"
)

// Allowlist is the set of URLs a researcher may fetch_url. Seed tweet links are added up front;
// URLs from Anthropic web_search_tool_result blocks are added as they arrive.
// Invented slugs fail here - the constraint lives in the action space, not the prompt.
type Allowlist struct {
	urls map[string]bool
}

func NewAllowlist() *Allowlist {
	return &Allowlist{urls: map[string]bool{}}
}

// Allow registers one URL. Trailing punctuation is stripped so links copied from
// tweet text meatch the form fetch_url will be called with.
func (a *Allowlist) Allow(raw string) {
	if a == nil {
		return
	}
	if u := normalizeFetchURL(raw); u != "" {
		a.urls[u] = true
	}
}

// Allowed reports whether fetch_url may GET this URL. An archive.org snapshot of an
// already-allowed URL is allowed; an archive of anything else is not.
func (a *Allowlist) Allowed(raw string) bool {
	if a == nil {
		return true // no gate configured
	}
	u := normalizeFetchURL(raw)
	if u == "" {
		return false
	}
	if a.urls[u] {
		return true
	}
	if orig, ok := archiveOriginal(u); ok {
		return a.urls[normalizeFetchURL(orig)]
	}
	return false
}

// AddFromContent harvests URLs from web_search_tool_result blocks in an assistant turn.
func (a *Allowlist) AddFromContent(blocks []ai.ContentBlock) {
	if a == nil {
		return
	}
	for _, b := range blocks {
		if b.Type != "web_search_tool_result" || len(b.Raw) == 0 {
			continue
		}
		var parsed struct {
			Content []struct {
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"content"`
		}
		if err := json.Unmarshal(b.Raw, &parsed); err != nil {
			continue
		}
		for _, item := range parsed.Content {
			if item.Type == "web_search_result" {
				a.Allow(item.URL)
			}
		}
	}
}

// Clone returns a shallow copy so paralel researchers can mutate their own
// search-result allowlists without racing on the seed set.
func (a *Allowlist) Clone() *Allowlist {
	if a == nil {
		return nil
	}
	out := NewAllowlist()
	for u := range a.urls {
		out.urls[u] = true
	}
	return out
}

func normalizeFetchURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), ".,;:!?)\"")
}

// arhiveOriginal pulls the original URL out of a web.archive.org/web/<stamp>/<url> form.
func archiveOriginal(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(u.Host)
	if host != "web.archive.org" && !strings.HasSuffix(host, ".web.archive.org") {
		return "", false
	}
	// path: /web/20260701000000/https://example.com/story
	path := u.Path
	const prefix = "/web/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest := path[len(prefix):]
	slash := strings.IndexByte(rest, '/')
	if slash < 0 || slash+1 >= len(rest) {
		return "", false
	}
	orig := rest[slash+1:]
	if !strings.HasPrefix(orig, "http://") && !strings.HasPrefix(orig, "https://") {
		return "", false
	}
	return orig, true
}

// GatedFetch wraps a ToolSource and refuses fetch_url calls whose URL is not allowlisted.
// Other tools pass through unchanged.
type GatedFetch struct {
	Inner ToolSource
	Allow *Allowlist
}

func (g GatedFetch) Tools(ctx context.Context) ([]ai.ToolDef, error) {
	return g.Inner.Tools(ctx)
}
func (g GatedFetch) Call(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	if name == "fetch_url" && g.Allow != nil {
		var in struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.URL) == "" {
			return "fetch_url requires a url string", true, nil
		}
		if !g.Allow.Allowed(in.URL) {
			return fmt.Sprintf(
				"url %q is not on the allowlist — fetch only seed-tweet links or URLs returned by web_search",
				in.URL,
			), true, nil
		}
	}
	return g.Inner.Call(ctx, name, args)
}
