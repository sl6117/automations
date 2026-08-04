package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicComplete(t *testing.T) {
	var gotAPIKey, gotVersion string
	var gotBody anthropicRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"model": "claude-haiku-4.5",
			"content": [{"type": "text", "text": "hello digest"}],
			"usage": {"input_tokens": 12, "output_tokens": 5}
		}`)
	}))
	defer server.Close()

	client := Anthropic{APIKey: "test-key", BaseURL: server.URL}
	response, err := client.Complete(context.Background(), Request{
		Model:     "claude-haiku-4.5",
		System:    "you're a digest writer",
		Prompt:    "summarize these tweets",
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if response.Text != "hello digest" {
		t.Errorf("Text = %q, want hello digest", response.Text)
	}
	if response.Model != "claude-haiku-4.5" {
		t.Errorf("Model = %q, want claude-haiku-4.5", response.Model)
	}
	if response.Usage.InputTokens != 12 || response.Usage.OutputTokens != 5 {
		t.Errorf("Usage = %+v, want {InputTokens: 12, OutputTokens: 5}", response.Usage)
	}
	if gotAPIKey != "test-key" {
		t.Errorf("x-api-key = %q, want test-key", gotAPIKey)
	}
	if gotVersion != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want 2023-06-01", gotVersion)
	}
	if gotBody.System != "you're a digest writer" {
		t.Errorf("request system = %q, want it set as a top-level field", gotBody.System)
	}
	if len(gotBody.Messages) == 0 || !strings.Contains(gotBody.Messages[len(gotBody.Messages)-1].Content, "summarize") {
		t.Errorf("request prompt missing; got %+v", gotBody.Messages)
	}

}

// A completion cut off at max_tokens is indistinguishable from a finished one by its text
// alone: it just ends mid-sentence. Callers cannot decide whether to trust the output
// unless the stop reason survives the adapter.
func TestAnthropicCompleteSurfacesTruncation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"model": "claude-haiku-4.5",
			"stop_reason": "max_tokens",
			"content": [{"type": "text", "text": "## AI\n- a story that stops mid-sen"}],
			"usage": {"input_tokens": 12, "output_tokens": 1500}
		}`)
	}))
	defer server.Close()

	client := Anthropic{APIKey: "test-key", BaseURL: server.URL}
	response, err := client.Complete(context.Background(), Request{Model: "claude-haiku-4.5", Prompt: "summarize", MaxTokens: 1500})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if response.StopReason != StopMaxTokens {
		t.Errorf("StopReason = %q, want %q", response.StopReason, StopMaxTokens)
	}
}

// The ordinary case must not look truncated, or every run trips the alarm.
func TestAnthropicCompleteReportsNormalStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"model": "claude-haiku-4.5",
			"stop_reason": "end_turn",
			"content": [{"type": "text", "text": "hello digest"}],
			"usage": {"input_tokens": 12, "output_tokens": 5}
		}`)
	}))
	defer server.Close()

	client := Anthropic{APIKey: "test-key", BaseURL: server.URL}
	response, err := client.Complete(context.Background(), Request{Model: "claude-haiku-4.5", Prompt: "summarize", MaxTokens: 1500})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if response.StopReason == StopMaxTokens {
		t.Errorf("StopReason = %q, a completed answer must not read as truncated", response.StopReason)
	}
}

func TestAnthropicChat(t *testing.T) {
	var gotBody anthropicChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"model": "claude-haiku-4-5",
			"stop_reason": "tool_use",
			"content": [
				{"type": "text", "text": "let me check"},
				{"type": "tool_use", "id": "tu_1", "name": "list_runs", "input": {"since": "2026-07-14"}}
			],
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`)
	}))
	defer server.Close()
	client := Anthropic{APIKey: "test-key", BaseURL: server.URL}
	resp, err := client.Chat(context.Background(), ChatRequest{
		Model:  "claude-haiku-4-5",
		System: "you answer questions about digest runs",
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{{Type: "text", Text: "which runs happened this week?"}}},
		},
		Tools: []ToolDef{{
			Name:        "list_runs",
			Description: "list digest run artifact keys",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"since":{"type":"string"}}}`),
		}},
		MaxTokens: 200,
	})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	// response direction: wire JSON -> Go types
	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason = %q, want tool_use", resp.StopReason)
	}
	if len(resp.Content) != 2 {
		t.Fatalf("Content blocks = %d, want 2", len(resp.Content))
	}
	tu := resp.Content[1]
	if tu.Type != "tool_use" || tu.ID != "tu_1" || tu.Name != "list_runs" {
		t.Errorf("tool_use block = %+v, want id tu_1 calling list_runs", tu)
	}
	if !strings.Contains(string(tu.Input), "2026-07-14") {
		t.Errorf("Input = %s, want the since argument preserved", tu.Input)
	}
	// request direction: Go types -> wire JSON
	if len(gotBody.Tools) != 1 || gotBody.Tools[0].Name != "list_runs" {
		t.Errorf("wire tools = %+v, want list_runs", gotBody.Tools)
	}
	if !strings.Contains(string(gotBody.Tools[0].InputSchema), `"since"`) {
		t.Errorf("wire input_schema = %s, want the schema passed through", gotBody.Tools[0].InputSchema)
	}
	if len(gotBody.Messages) != 1 || len(gotBody.Messages[0].Content) != 1 {
		t.Fatalf("wire messages = %+v, want one user turn with one content block", gotBody.Messages)
	}
	var userBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(gotBody.Messages[0].Content[0], &userBlock); err != nil {
		t.Fatalf("user content block: %v", err)
	}
	if userBlock.Type != "text" || userBlock.Text != "which runs happened this week?" {
		t.Errorf("wire user block = %+v, want text asking which runs happened this week", userBlock)
	}
	if resp.Text != "let me check" {
		t.Errorf("Text = %q, want concatenated text blocks", resp.Text)
	}
}

// Anthropic web_search is a server tool: the API runs it, so the wire entry carries a
// type + max_uses and no input_schema. Client tools keep the existing shape.
func TestAnthropicChatWiresServerTools(t *testing.T) {
	var gotBody map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"model": "claude-haiku-4-5",
			"stop_reason": "end_turn",
			"content": [{"type": "text", "text": "done"}],
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`)
	}))
	defer server.Close()

	client := Anthropic{APIKey: "test-key", BaseURL: server.URL}
	_, err := client.Chat(context.Background(), ChatRequest{
		Model: "claude-haiku-4-5",
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{{Type: "text", Text: "search then fetch"}}},
		},
		Tools: []ToolDef{{
			Name:        "fetch_url",
			Description: "GET a URL",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}`),
		}},
		ServerTools: []ServerTool{{
			Type:    WebSearchToolType,
			Name:    "web_search",
			MaxUses: 3,
		}},
		MaxTokens: 200,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	var tools []map[string]any
	if err := json.Unmarshal(gotBody["tools"], &tools); err != nil {
		t.Fatalf("tools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2 (client + server)", len(tools))
	}
	if tools[0]["name"] != "fetch_url" || tools[0]["input_schema"] == nil {
		t.Errorf("client tool = %#v, want fetch_url with input_schema", tools[0])
	}
	if tools[0]["type"] != nil {
		t.Errorf("client tool must not force a type field: %#v", tools[0])
	}
	if tools[1]["type"] != WebSearchToolType || tools[1]["name"] != "web_search" {
		t.Errorf("server tool = %#v, want type=%s name=web_search", tools[1], WebSearchToolType)
	}
	if tools[1]["max_uses"] != float64(3) {
		t.Errorf("max_uses = %v, want 3", tools[1]["max_uses"])
	}
	if _, ok := tools[1]["input_schema"]; ok {
		t.Errorf("server tool must not carry input_schema: %#v", tools[1])
	}
}

// web_search_tool_result content is a nested array with encrypted_content the API requires
// on later turns. A typed Content string cannot hold that; Raw passthrough must.
func TestAnthropicChatPreservesServerToolResultBlocks(t *testing.T) {
	searchResult := `{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[{"type":"web_search_result","url":"https://example.com/story","title":"Story","encrypted_content":"enc-abc","page_age":"3 days ago"}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"model": "claude-haiku-4-5",
			"stop_reason": "tool_use",
			"content": [
				{"type": "server_tool_use", "id": "srv_1", "name": "web_search", "input": {"query": "Clarity Act"}},
				`+searchResult+`,
				{"type": "tool_use", "id": "tu_1", "name": "fetch_url", "input": {"url": "https://example.com/story"}}
			],
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`)
	}))
	defer server.Close()

	client := Anthropic{APIKey: "test-key", BaseURL: server.URL}
	resp, err := client.Chat(context.Background(), ChatRequest{
		Model:     "claude-haiku-4-5",
		Messages:  []Message{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "corroborate"}}}},
		MaxTokens: 200,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.Content) != 3 {
		t.Fatalf("content blocks = %d, want 3", len(resp.Content))
	}
	if resp.Content[0].Type != "server_tool_use" || resp.Content[0].Raw == nil {
		t.Errorf("server_tool_use = %+v, want Type set and Raw preserved", resp.Content[0])
	}
	if resp.Content[1].Type != "web_search_tool_result" || !strings.Contains(string(resp.Content[1].Raw), "enc-abc") {
		t.Errorf("web_search_tool_result Raw = %s, want encrypted_content kept", resp.Content[1].Raw)
	}
	if resp.Content[2].Type != "tool_use" || resp.Content[2].Name != "fetch_url" {
		t.Errorf("client tool_use = %+v", resp.Content[2])
	}

	// Round-trip: the next request must echo the server blocks byte-faithfully enough
	// that encrypted_content survives — otherwise Anthropic rejects the continuation.
	var gotBody struct {
		Messages []struct {
			Content []json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	roundTrip := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"m","stop_reason":"end_turn","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer roundTrip.Close()

	client.BaseURL = roundTrip.URL
	_, err = client.Chat(context.Background(), ChatRequest{
		Model: "claude-haiku-4-5",
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{{Type: "text", Text: "corroborate"}}},
			{Role: "assistant", Content: resp.Content},
			{Role: "user", Content: []ContentBlock{{Type: "tool_result", ToolUseID: "tu_1", Content: `{"status":200}`}}},
		},
		MaxTokens: 200,
	})
	if err != nil {
		t.Fatalf("round-trip Chat: %v", err)
	}
	if len(gotBody.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(gotBody.Messages))
	}
	assistantWire := string(gotBody.Messages[1].Content[1])
	if !strings.Contains(assistantWire, "enc-abc") || !strings.Contains(assistantWire, "web_search_tool_result") {
		t.Errorf("assistant content[1] = %s, want web_search_tool_result with encrypted_content", assistantWire)
	}
}

// Top-level cache_control enables Anthropic automatic prompt caching: the API
// places a breakpoint on the last cacheable block and advances it as the
// multi-turn transcript grows. Haiku 4.5 needs ≥4096 tokens before a write
// sticks; below that the request still succeeds with zeros in the cache fields.
func TestAnthropicChatWiresPromptCache(t *testing.T) {
	var gotBody map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"model": "claude-haiku-4-5",
			"stop_reason": "end_turn",
			"content": [{"type": "text", "text": "ok"}],
			"usage": {
				"input_tokens": 40,
				"output_tokens": 5,
				"cache_creation_input_tokens": 4200,
				"cache_read_input_tokens": 0
			}
		}`)
	}))
	defer server.Close()

	client := Anthropic{APIKey: "test-key", BaseURL: server.URL}
	resp, err := client.Chat(context.Background(), ChatRequest{
		Model:  "claude-haiku-4-5",
		System: "stable system prompt",
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hello"}}},
		},
		PromptCache: true,
		MaxTokens:   100,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	var cc map[string]any
	if err := json.Unmarshal(gotBody["cache_control"], &cc); err != nil {
		t.Fatalf("cache_control missing or invalid: %v (body keys %v)", err, keysOf(gotBody))
	}
	if cc["type"] != "ephemeral" {
		t.Errorf("cache_control = %#v, want type=ephemeral", cc)
	}
	if resp.Usage.InputTokens != 40 || resp.Usage.CacheCreationInputTokens != 4200 || resp.Usage.CacheReadInputTokens != 0 {
		t.Errorf("Usage = %+v, want uncached=40 creation=4200 read=0", resp.Usage)
	}
	if resp.Usage.TotalInputTokens() != 4240 {
		t.Errorf("TotalInputTokens() = %d, want 4240", resp.Usage.TotalInputTokens())
	}
}

func TestAnthropicChatOmitsPromptCacheWhenOff(t *testing.T) {
	var gotBody map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"m","stop_reason":"end_turn","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer server.Close()

	client := Anthropic{APIKey: "test-key", BaseURL: server.URL}
	_, err := client.Chat(context.Background(), ChatRequest{
		Model: "claude-haiku-4-5",
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hi"}}},
		},
		MaxTokens: 50,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if _, ok := gotBody["cache_control"]; ok {
		t.Errorf("cache_control present when PromptCache is false: %s", gotBody["cache_control"])
	}
}

// tool_choice type=tool forces a specific client tool — the structured-output
// pattern: the model must call that tool, and tool_use.input is the contract.
func TestAnthropicChatWiresToolChoice(t *testing.T) {
	var gotBody map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"model": "claude-sonnet-4-5",
			"stop_reason": "tool_use",
			"content": [{
				"type": "tool_use",
				"id": "tu_1",
				"name": "submit_editor_report",
				"input": {"pass": true, "failures": []}
			}],
			"usage": {"input_tokens": 20, "output_tokens": 8}
		}`)
	}))
	defer server.Close()

	client := Anthropic{APIKey: "test-key", BaseURL: server.URL}
	resp, err := client.Chat(context.Background(), ChatRequest{
		Model: "claude-sonnet-4-5",
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{{Type: "text", Text: "judge this brief"}}},
		},
		Tools: []ToolDef{{
			Name:        "submit_editor_report",
			Description: "Submit the editor verdict",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"pass":{"type":"boolean"},"failures":{"type":"array","items":{"type":"string"}}},"required":["pass","failures"]}`),
		}},
		ToolChoice: &ToolChoice{Type: "tool", Name: "submit_editor_report"},
		MaxTokens:  200,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	var tc map[string]any
	if err := json.Unmarshal(gotBody["tool_choice"], &tc); err != nil {
		t.Fatalf("tool_choice missing or invalid: %v (body keys %v)", err, keysOf(gotBody))
	}
	if tc["type"] != "tool" || tc["name"] != "submit_editor_report" {
		t.Errorf("tool_choice = %#v, want type=tool name=submit_editor_report", tc)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason = %q, want tool_use", resp.StopReason)
	}
	if len(resp.Content) != 1 || resp.Content[0].Name != "submit_editor_report" {
		t.Errorf("Content = %+v, want one submit_editor_report tool_use", resp.Content)
	}
}

func TestAnthropicChatOmitsToolChoiceWhenNil(t *testing.T) {
	var gotBody map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"m","stop_reason":"end_turn","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer server.Close()

	client := Anthropic{APIKey: "test-key", BaseURL: server.URL}
	_, err := client.Chat(context.Background(), ChatRequest{
		Model: "claude-haiku-4-5",
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hi"}}},
		},
		MaxTokens: 50,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if _, ok := gotBody["tool_choice"]; ok {
		t.Errorf("tool_choice present when ToolChoice is nil: %s", gotBody["tool_choice"])
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
