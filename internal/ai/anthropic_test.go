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
	if len(gotBody.Messages) != 1 || len(gotBody.Messages[0].Content) != 1 || gotBody.Messages[0].Content[0].Text != "which runs happened this week?" {
		t.Errorf("wire messages = %+v, want the user turn as a text block", gotBody.Messages)
	}
}
