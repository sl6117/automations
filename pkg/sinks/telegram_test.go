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

func TestTelegramRedactsToken(t *testing.T) {
	telegram := Telegram{BotToken: "totallysecrettoken"}
	got := telegram.redact(`Post "https://api.telegram.org/bottotallysecrettoken/sendMessage": boom`)
	if strings.Contains(got, "totallysecrettoken") {
		t.Errorf("token leaked in error: %s", got)
	}
}

func TestTelegramDeliverFormatsHTML(t *testing.T) {
	var gotBody telegramSendMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		io.WriteString(w, `{"ok": true}`)
	}))
	defer server.Close()

	message := "## AI\n- Models & tools <3. @alice https://x.com/alice/status/1 and https://x.com/bob/status/2"
	telegram := Telegram{BotToken: "test-token", ChatID: "999", BaseURL: server.URL}
	if err := telegram.Deliver(context.Background(), message); err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}
	if gotBody.ParseMode != "HTML" {
		t.Errorf("parse_mode = %q, want HTML", gotBody.ParseMode)
	}
	want := "<b>AI</b>\n" +
		`- Models &amp; tools &lt;3. <a href="https://x.com/alice/status/1">@alice</a> and <a href="https://x.com/bob/status/2">link</a>`
	if gotBody.Text != want {
		t.Errorf("text =\n%q\nwant\n%q", gotBody.Text, want)
	}
}
