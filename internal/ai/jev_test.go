package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJevNouls(t *testing.T) {
	var gotAuth string
	var got jevRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"jev-test","answers":{"faithfulness":{"noul":0.91}}}`)
	}))
	defer server.Close()

	client := Jev{APIKey: "test-key", BaseURL: server.URL}
	gotAnswers, err := client.Nouls(context.Background(), "digest and tweets", map[string]string{
		"faithfulness": "Does the digest stay faithful to the tweets?",
	})
	if err != nil {
		t.Fatalf("Nouls: %v", err)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", gotAuth)
	}
	if got.State != "digest and tweets" {
		t.Errorf("state = %q", got.State)
	}
	if got.Model != "jev-1.13.0" {
		t.Errorf("model = %q, want jev-1.13.0", got.Model)
	}
	q, ok := got.Questions["faithfulness"]
	if !ok || q.Type != "noul" || q.Instructions == "" {
		t.Errorf("faithfulness question = %+v, want type noul and instructions", q)
	}
	if gotAnswers["faithfulness"] != 0.91 {
		t.Errorf("P(yes) = %v, want 0.91", gotAnswers["faithfulness"])
	}
}

func TestJevNoulsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	client := Jev{APIKey: "test-key", BaseURL: server.URL}
	_, err := client.Nouls(context.Background(), "state", map[string]string{"faithfulness": "faithful?"})
	if err == nil {
		t.Fatal("want error on status 400")
	}
}
