package sinks

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTelegramDeliver(t *testing.T) {

	var gotPath string
	var gotBody telegramSendMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		io.WriteString(w, `{"ok": true}`)
	}))
	defer server.Close()

	telegram := Telegram{BotToken: "test-token", ChatID: "999", BaseURL: server.URL}
	if err := telegram.Deliver(context.Background(), "hello digest"); err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	if gotPath != "/bottest-token/sendMessage" {
		t.Errorf("path = %q, want /bottest-token/sendMessage", gotPath)
	}
	if gotBody.ChatID != "999" {
		t.Errorf("chat_id = %q, want 999", gotBody.ChatID)
	}
	if gotBody.Text != "hello digest" {
		t.Errorf("text = %q, want hello digest", gotBody.Text)
	}
	if gotBody.DisableWebPagePreview != true {
		t.Errorf("disable_web_page_preview = %t, want true", gotBody.DisableWebPagePreview)
	}
}

func TestTelegramDeliverChunks(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, `{"ok": true}`)
	}))
	defer server.Close()

	longLine := strings.Repeat("x", 1500)
	message := strings.Join([]string{longLine, longLine, longLine, longLine}, "\n")

	telegram := Telegram{BotToken: "test-token", ChatID: "1", BaseURL: server.URL}

	if err := telegram.Deliver(context.Background(), message); err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}
	if calls < 2 {
		t.Errorf("expected the long message to be split into multiple sends, got %d", calls)
	}
}
