package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// When PromptCache is on and System is set, Complete must send system as a
// content-block array with cache_control on that block — so a stable prefix
// (instructions + tweets) can be written once and read on later Complete calls
// that only change the user message (e.g. digest language).
func TestAnthropicCompleteWiresSystemCacheControl(t *testing.T) {
	var gotBody map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"model": "claude-haiku-4-5",
			"content": [{"type": "text", "text": "ok"}],
			"usage": {
				"input_tokens": 20,
				"output_tokens": 5,
				"cache_creation_input_tokens": 5000,
				"cache_read_input_tokens": 0
			}
		}`)
	}))
	defer server.Close()

	client := Anthropic{APIKey: "test-key", BaseURL: server.URL}
	resp, err := client.Complete(context.Background(), Request{
		Model:       "claude-haiku-4-5",
		System:      "stable digest instructions and tweets",
		Prompt:      "Write every summary in English.",
		PromptCache: true,
		MaxTokens:   100,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var system []map[string]any
	if err := json.Unmarshal(gotBody["system"], &system); err != nil {
		t.Fatalf("system must be a content-block array when PromptCache is on: %v (%s)", err, gotBody["system"])
	}
	if len(system) != 1 || system[0]["type"] != "text" {
		t.Fatalf("system = %#v, want one text block", system)
	}
	if system[0]["text"] != "stable digest instructions and tweets" {
		t.Errorf("system text = %v", system[0]["text"])
	}
	cc, ok := system[0]["cache_control"].(map[string]any)
	if !ok || cc["type"] != "ephemeral" {
		t.Errorf("cache_control = %#v, want ephemeral on the system block", system[0]["cache_control"])
	}
	if resp.Usage.CacheCreationInputTokens != 5000 || resp.Usage.InputTokens != 20 {
		t.Errorf("Usage = %+v, want creation=5000 uncached=20", resp.Usage)
	}
}

func TestAnthropicCompleteKeepsStringSystemWhenCacheOff(t *testing.T) {
	var gotBody map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"m","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer server.Close()

	client := Anthropic{APIKey: "test-key", BaseURL: server.URL}
	_, err := client.Complete(context.Background(), Request{
		Model:  "claude-haiku-4-5",
		System: "plain system",
		Prompt: "hi",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var sys string
	if err := json.Unmarshal(gotBody["system"], &sys); err != nil || sys != "plain system" {
		t.Errorf("system = %s, want plain string when PromptCache is false", gotBody["system"])
	}
}
