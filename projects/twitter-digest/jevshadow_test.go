package twitterdigest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/pkg/sources"
)

func TestJevShadowAsksFourDimensions(t *testing.T) {
	var got aiBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"jev-test","answers":{"faithfulness":{"noul":0.2},"topicRouting":{"noul":0.9},"coverage":{"noul":0.8},"clarity":{"noul":0.7}}}`)
	}))
	defer server.Close()

	scores, err := jevShadow(context.Background(), ai.Jev{APIKey: "test-key", BaseURL: server.URL},
		[]Topic{{Name: "AI", Description: "models"}},
		[]sources.Tweet{{Handle: "@darioa", Text: "AI will be powerful"}},
		"## AI\n- big story", "English")
	if err != nil {
		t.Fatalf("jevShadow: %v", err)
	}
	if scores["faithfulness"] != 0.2 || scores["clarity"] != 0.7 {
		t.Errorf("scores = %v", scores)
	}
	if len(got.Questions) != 4 {
		t.Errorf("asked %d questions, want 4", len(got.Questions))
	}
	if !strings.Contains(got.State, "AI will be powerful") || !strings.Contains(got.State, "big story") {
		t.Errorf("state missing tweet or digest: %s", got.State)
	}
}

type aiBody struct {
	State     string         `json:"state"`
	Questions map[string]any `json:"questions"`
}
