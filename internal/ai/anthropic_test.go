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
