package twitterdigest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/pkg/sources"
)

func jevAnswers(body []byte, noul float64) string {
	var req struct {
		Questions map[string]json.RawMessage `json:"questions"`
	}
	_ = json.Unmarshal(body, &req)
	answers := make(map[string]map[string]float64, len(req.Questions))
	for name := range req.Questions {
		answers[name] = map[string]float64{"noul": noul}
	}
	out, err := json.Marshal(map[string]any{"model": "jev-test", "answers": answers})
	if err != nil {
		return ""
	}
	return string(out)
}

func TestJevScoresDraftOnceWhenTextIsUnchanged(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "original story") {
			t.Errorf("scored text is not the first draft: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, jevAnswers(body, 0.2))
	}))
	defer server.Close()

	a := runFunnel(t, []string{passAllJSON}, ai.Jev{APIKey: "test-key", BaseURL: server.URL})
	if calls != 1 {
		t.Fatalf("jev calls = %d, want 1 when revise does not change the text", calls)
	}
	if a.JevScale != jevScaleDefect {
		t.Errorf("jevScale = %q, want %q", a.JevScale, jevScaleDefect)
	}
	if a.InitialJev["faithfulness"] != 0.2 || a.Jev["faithfulness"] != 0.2 {
		t.Errorf("draft jev = %v sent jev = %v, want both 0.2", a.InitialJev, a.Jev)
	}
	if a.InitialJevDetail["b0_fact"] != 0.2 || a.JevDetail["b0_fact"] != 0.2 {
		t.Errorf("detail draft = %v sent = %v, want the bullet fact question", a.InitialJevDetail, a.JevDetail)
	}
	if a.InitialJevError != "" || a.JevError != "" {
		t.Errorf("errors = %q %q, want empty", a.InitialJevError, a.JevError)
	}
}

func TestJevScoresDraftAndSentDigestSeparately(t *testing.T) {
	var sawDraft, sawSent bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		faith := 0.11
		switch {
		case strings.Contains(text, "original story"):
			sawDraft = true
		case strings.Contains(text, "revised story"):
			sawSent = true
			faith = 0.88
		default:
			t.Errorf("scored unexpected text: %s", text)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, jevAnswers([]byte(text), faith))
	}))
	defer server.Close()

	a := runFunnel(t, []string{failFaithJSON, passAllJSON}, ai.Jev{APIKey: "test-key", BaseURL: server.URL})
	if !sawDraft || !sawSent {
		t.Fatalf("sawDraft=%v sawSent=%v, want both texts scored", sawDraft, sawSent)
	}
	if a.InitialJev["faithfulness"] != 0.11 {
		t.Errorf("draft faithfulness = %v, want 0.11", a.InitialJev["faithfulness"])
	}
	if a.Jev["faithfulness"] != 0.88 {
		t.Errorf("sent faithfulness = %v, want 0.88", a.Jev["faithfulness"])
	}
	if !a.RevisionAdopted {
		t.Error("revisionAdopted = false, want the revised digest to have shipped")
	}
}

func TestJevDraftErrorDoesNotBlockSentScore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "original story") {
			http.Error(w, "unavailable", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, jevAnswers(body, 0.88))
	}))
	defer server.Close()

	a := runFunnel(t, []string{failFaithJSON, passAllJSON}, ai.Jev{APIKey: "test-key", BaseURL: server.URL})
	if a.InitialJev != nil || a.InitialJevError == "" {
		t.Errorf("draft jev = %v error = %q, want a recorded error and no scores", a.InitialJev, a.InitialJevError)
	}
	if a.Jev["faithfulness"] != 0.88 || a.JevError != "" {
		t.Errorf("sent jev = %v error = %q, want the revised digest scored", a.Jev, a.JevError)
	}
}

func TestJevShadowAsksEachBulletAndTweet(t *testing.T) {
	var got aiBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.Header().Set("Content-Type", "application/json")
		answers := make(map[string]map[string]float64, len(got.Questions))
		for name := range got.Questions {
			noul := 0.1
			switch name {
			case "b0_cite":
				noul = 0.4
			case "b1_merged":
				noul = 0.7
			case "t1_missing":
				noul = 0.55
			case "split":
				noul = 0.3
			}
			answers[name] = map[string]float64{"noul": noul}
		}
		out, _ := json.Marshal(map[string]any{"model": "jev-test", "answers": answers})
		w.Write(out)
	}))
	defer server.Close()

	scored, err := jevShadow(context.Background(), ai.Jev{APIKey: "test-key", BaseURL: server.URL},
		[]Topic{{Name: "AI", Description: "models"}},
		[]sources.Tweet{{Handle: "@darioa", Text: "AI will be powerful"}, {Handle: "@other", Text: "markets moved"}},
		"## AI\n- big story\n## Econ\n- second story", "English")
	if err != nil {
		t.Fatalf("jevShadow: %v", err)
	}
	if len(got.Questions) != 2*5+1+2 {
		t.Errorf("asked %d questions, want 13 (5 per bullet, split, 1 per tweet)", len(got.Questions))
	}
	fact, _ := got.Questions["b0_fact"].(map[string]any)
	if !strings.Contains(fmt.Sprint(fact["instructions"]), "big story") {
		t.Errorf("b0_fact = %v, want the first bullet quoted", fact)
	}
	missing, _ := got.Questions["t1_missing"].(map[string]any)
	if !strings.Contains(fmt.Sprint(missing["instructions"]), "markets moved") {
		t.Errorf("t1_missing = %v, want the second tweet quoted", missing)
	}
	if !strings.Contains(got.State, "AI will be powerful") || !strings.Contains(got.State, "big story") {
		t.Errorf("state missing tweet or digest: %s", got.State)
	}
	if scored.Rollup["faithfulness"] != 0.4 || scored.Rollup["coverage"] != 0.55 || scored.Rollup["clarity"] != 0.7 {
		t.Errorf("rollup = %v, want worst defect per dimension", scored.Rollup)
	}
}

func TestJevRollupIsZeroWithoutQuestions(t *testing.T) {
	got := jevRollup(map[string]float64{"split": 0.2})
	if got["faithfulness"] != 0 || got["coverage"] != 0 || got["clarity"] != 0.2 {
		t.Errorf("rollup = %v", got)
	}
}

type aiBody struct {
	State     string         `json:"state"`
	Questions map[string]any `json:"questions"`
}
