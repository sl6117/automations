package ai

import (
	"context"
	"testing"

	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

func TestOpenRouterComplete(t *testing.T) {
	var gotAuth string
	var gotBody chatRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"choices": [{"message": {"role": "assistant", "content": "hello digest"}}],
			"usage": {"prompt_tokens": 12, "completion_tokens":5}
		}`)

	}))
	defer server.Close()

	client := OpenRouter{APIKey: "test-key", BaseURL: server.URL}
	response, err := client.Complete(context.Background(), Request{
		Model:     "anthropic/claude-haiku-4.5",
		Prompt:    "summarize these tweets",
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if response.Text != "hello digest" {
		t.Errorf("Text = %q, want hello digest", response.Text)
	}
	if response.Usage.InputTokens != 12 || response.Usage.OutputTokens != 5 {
		t.Errorf("Usage = %+v, want {InputTokens: 12, OutputTokens: 5}", response.Usage)
	}

	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-key")
	}

	if gotBody.Model != "anthropic/claude-haiku-4.5" {
		t.Errorf("request model = %q, want %q", gotBody.Model, "anthropic/claude-haiku-4.5")
	}
	if len(gotBody.Messages) == 0 || !strings.Contains(gotBody.Messages[len(gotBody.Messages)-1].Content, "summarize") {
		t.Errorf("request prompt missing; got %+v", gotBody.Messages)
	}
}
