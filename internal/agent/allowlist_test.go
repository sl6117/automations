package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/ai"
)

func TestAllowlistAllowsSeedsAndSearchResults(t *testing.T) {
	a := NewAllowlist()
	a.Allow("https://t.co/abc123")
	a.Allow("https://example.com/story.")

	if !a.Allowed("https://t.co/abc123") {
		t.Error("seed URL should be allowed")
	}
	// trailing punctuation stripped the same way renderSeeds does
	if !a.Allowed("https://example.com/story") {
		t.Error("normalized seed URL should be allowed")
	}
	if a.Allowed("https://evil.example/invented") {
		t.Error("unknown URL must be denied")
	}
}

// Archive fallback is the existing researcher escape hatch for paywalls. It must only
// wrap an already-allowed URL — otherwise inventing archive.org/web/*/reuters.com/...
// would reopen the hole search was meant to close.
func TestAllowlistAllowsArchiveOfAllowedURL(t *testing.T) {
	a := NewAllowlist()
	a.Allow("https://example.com/story")

	if !a.Allowed("https://web.archive.org/web/20260701000000/https://example.com/story") {
		t.Error("archive of an allowed URL should be allowed")
	}
	if a.Allowed("https://web.archive.org/web/20260701000000/https://evil.example/nope") {
		t.Error("archive of an unknown URL must be denied")
	}
}

func TestAllowlistAddFromSearchContent(t *testing.T) {
	a := NewAllowlist()
	blocks := []ai.ContentBlock{
		{Type: "server_tool_use", Raw: json.RawMessage(`{"type":"server_tool_use","id":"srv_1","name":"web_search"}`)},
		{Type: "web_search_tool_result", Raw: json.RawMessage(`{
			"type":"web_search_tool_result",
			"tool_use_id":"srv_1",
			"content":[
				{"type":"web_search_result","url":"https://reuters.com/a","title":"A","encrypted_content":"enc"},
				{"type":"web_search_result","url":"https://apnews.com/b","title":"B","encrypted_content":"enc2"}
			]
		}`)},
		{Type: "tool_use", ID: "tu_1", Name: "fetch_url", Input: json.RawMessage(`{"url":"https://reuters.com/a"}`)},
	}
	a.AddFromContent(blocks)

	if !a.Allowed("https://reuters.com/a") || !a.Allowed("https://apnews.com/b") {
		t.Errorf("search result URLs not allowed: reuters=%v ap=%v", a.Allowed("https://reuters.com/a"), a.Allowed("https://apnews.com/b"))
	}
}

func TestGatedFetchDeniesUnknownURL(t *testing.T) {
	inner := &fakeTools{result: `{"status":200}`}
	allow := NewAllowlist()
	allow.Allow("https://ok.example/a")
	gated := GatedFetch{Inner: inner, Allow: allow}

	out, isErr, err := gated.Call(context.Background(), "fetch_url", json.RawMessage(`{"url":"https://evil.example/x"}`))
	if err != nil {
		t.Fatalf("deny must be a tool-level error, not transport: %v", err)
	}
	if !isErr {
		t.Error("isError = false, want true so the model can recover")
	}
	if !strings.Contains(out, "not on the allowlist") {
		t.Errorf("result = %q, want an allowlist explanation", out)
	}
	if len(inner.calls) != 0 {
		t.Errorf("inner was called %v; deny must happen before MCP", inner.calls)
	}
}

func TestGatedFetchAllowsListedURL(t *testing.T) {
	inner := &fakeTools{result: `{"status":200,"body":"ok"}`}
	allow := NewAllowlist()
	allow.Allow("https://ok.example/a")
	gated := GatedFetch{Inner: inner, Allow: allow}

	out, isErr, err := gated.Call(context.Background(), "fetch_url", json.RawMessage(`{"url":"https://ok.example/a"}`))
	if err != nil || isErr {
		t.Fatalf("allowed fetch failed: out=%q isErr=%v err=%v", out, isErr, err)
	}
	if len(inner.calls) != 1 {
		t.Errorf("inner calls = %v, want one fetch_url", inner.calls)
	}
}

// Non-fetch tools (list_runs, get_artifact) must stay ungated — the planner uses them
// and they are not a URL-invention surface.
func TestGatedFetchPassesThroughOtherTools(t *testing.T) {
	inner := &fakeTools{result: `{"keys":[]}`}
	gated := GatedFetch{Inner: inner, Allow: NewAllowlist()}

	_, isErr, err := gated.Call(context.Background(), "list_runs", json.RawMessage(`{}`))
	if err != nil || isErr {
		t.Fatalf("list_runs: isErr=%v err=%v", isErr, err)
	}
	if len(inner.calls) != 1 || !strings.HasPrefix(inner.calls[0], "list_runs") {
		t.Errorf("calls = %v, want list_runs passed through", inner.calls)
	}
}

// After a server-side search, the model may fetch_url a result URL on the same turn.
// The loop must harvest search URLs into the allowlist before executing client tools.
func TestRunAllowlistsSearchResultsBeforeFetch(t *testing.T) {
	allow := NewAllowlist()
	chat := &fakeChat{responses: []ai.ChatResponse{
		{
			StopReason: "tool_use",
			Content: []ai.ContentBlock{
				{Type: "web_search_tool_result", Raw: json.RawMessage(`{
					"type":"web_search_tool_result","tool_use_id":"srv_1",
					"content":[{"type":"web_search_result","url":"https://reuters.com/story","encrypted_content":"enc"}]
				}`)},
				{Type: "tool_use", ID: "tu_1", Name: "fetch_url", Input: json.RawMessage(`{"url":"https://reuters.com/story"}`)},
			},
			Usage: ai.Usage{InputTokens: 5, OutputTokens: 5},
		},
		textResp(`{"question":"q","findings":["f"],"sources":["https://reuters.com/story"],"corroborated":true}`),
	}}
	inner := &fakeTools{result: `{"status":200}`}
	tools := GatedFetch{Inner: inner, Allow: allow}

	_, err := Run(context.Background(), Config{
		Client: chat, Tools: tools, Model: "m", MaxTokens: 100, MaxToolTurns: 3,
		WebSearchMaxUses: 2,
		FetchAllowlist:   allow,
	}, "corroborate")
	if err != nil {
		t.Fatal(err)
	}
	if !allow.Allowed("https://reuters.com/story") {
		t.Error("search result URL was not allowlisted before fetch")
	}
	if len(inner.calls) != 1 {
		t.Errorf("inner calls = %v, want the allowlisted fetch to proceed", inner.calls)
	}
}

func TestAllowlistCloneIndependent(t *testing.T) {
	a := NewAllowlist()
	a.Allow("https://example.com/seed")
	b := a.Clone()
	b.Allow("https://example.com/from-search")
	if !a.Allowed("https://example.com/seed") {
		t.Fatal("original lost seed")
	}
	if a.Allowed("https://example.com/from-search") {
		t.Fatal("clone write must not mutate original")
	}
	if !b.Allowed("https://example.com/seed") || !b.Allowed("https://example.com/from-search") {
		t.Fatal("clone should have seed + new URL")
	}
}

func TestAllowlistCloneNil(t *testing.T) {
	var a *Allowlist
	if a.Clone() != nil {
		t.Fatal("nil clone should be nil")
	}
}
