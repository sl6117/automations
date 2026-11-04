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
	gotAnswers, err := client.Nouls(context.Background(), "digest and tweets", map[string]Noul{
		"faithfulness": {Instructions: "Does the digest stay faithful to the tweets?"},
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
	if q.Criteria != nil {
		t.Errorf("criteria = %+v, want omitted", q.Criteria)
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
	_, err := client.Nouls(context.Background(), "state", map[string]Noul{"faithfulness": {Instructions: "faithful?"}})
	if err == nil {
		t.Fatal("want error on status 400")
	}
}

func TestJevNoulsSendsCriteria(t *testing.T) {
	var got jevRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"jev-test","answers":{"b0_attr":{"noul":0.11}}}`)
	}))
	defer server.Close()

	client := Jev{APIKey: "test-key", BaseURL: server.URL}
	_, err := client.Nouls(context.Background(), "digest", map[string]Noul{
		"b0_attr": {
			Instructions: "Does this bullet drop who said the claim?",
			Criteria: &NoulCriteria{
				True:  `The bullet states the claim with no speaker and no "according to the source."`,
				False: "The bullet names the speaker, or says the claim is according to the source.",
			},
		},
	})
	if err != nil {
		t.Fatalf("Nouls: %v", err)
	}
	q := got.Questions["b0_attr"]
	if q.Criteria == nil || q.Criteria.True == "" || !strings.Contains(q.Criteria.False, "names the speaker") {
		t.Fatalf("criteria = %+v, want true and false", q.Criteria)
	}
}
