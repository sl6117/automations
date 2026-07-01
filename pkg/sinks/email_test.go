package sinks

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmailDeliver(t *testing.T) {
	var gotAuth string
	var gotBody resendEmail

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"id": "abc123"}`)
	}))
	defer server.Close()

	email := Email{
		APIKey:  "test-key",
		From:    "digest@example.com",
		To:      []string{"me@example.com", "friend@example.com"},
		Subject: "Daily X Digest",
		BaseURL: server.URL,
	}

	if err := email.Deliver(context.Background(), "hello digest"); err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", gotAuth)
	}
	if gotBody.From != "digest@example.com" {
		t.Errorf("from = %q, want digest@example.com", gotBody.From)
	}
	if len(gotBody.To) != 2 || gotBody.To[0] != "me@example.com" {
		t.Errorf("to = %v, want two recipients", gotBody.To)
	}

	if gotBody.Subject != "Daily X Digest" {
		t.Errorf("subject = %q, want Daily X Digest", gotBody.Subject)
	}
	if gotBody.Text != "hello digest" {
		t.Errorf("text = %q, want hello digest", gotBody.Text)
	}
}

func TestEmailDeliverNoRecipeints(t *testing.T) {
	email := Email{APIKey: "test-key", From: "d@example.com", Subject: "x"}
	if err := email.Deliver(context.Background(), "hello"); err == nil {
		t.Fatalf("Deliver should fail, got nil")
	}
}
