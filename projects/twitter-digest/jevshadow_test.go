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
	if a.JevRoute != "send" {
		t.Errorf("jevRoute = %q, want send when every draft defect is 0.2", a.JevRoute)
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
	if a.JevRoute != "send" {
		t.Errorf("jevRoute = %q, want send from the draft score 0.11, not the sent score", a.JevRoute)
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
	if a.JevRoute != "escalate" {
		t.Errorf("jevRoute = %q, want escalate when the draft call fails", a.JevRoute)
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
			case "b0_again":
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
	if len(got.Questions) != 2*7+2 {
		t.Errorf("asked %d questions, want 16 (7 per bullet, 1 per tweet)", len(got.Questions))
	}
	fact, _ := got.Questions["b0_fact"].(map[string]any)
	if !strings.Contains(fmt.Sprint(fact["instructions"]), "big story") {
		t.Errorf("b0_fact = %v, want the first bullet quoted", fact)
	}
	again, _ := got.Questions["b0_again"].(map[string]any)
	if !strings.Contains(fmt.Sprint(again["instructions"]), "big story") || !strings.Contains(fmt.Sprint(again["instructions"]), "same event") {
		t.Errorf("b0_again = %v, want the first bullet and the same-event question", again)
	}
	hedge, _ := got.Questions["b0_hedge"].(map[string]any)
	if !strings.Contains(fmt.Sprint(hedge["instructions"]), "big story") || !strings.Contains(fmt.Sprint(hedge["instructions"]), "on track") {
		t.Errorf("b0_hedge = %v, want the first bullet and the hedge question", hedge)
	}

	attr, _ := got.Questions["b0_attr"].(map[string]any)
	criteria, _ := attr["criteria"].(map[string]any)
	if !strings.Contains(fmt.Sprint(attr["instructions"]), "big story") || strings.Contains(fmt.Sprint(attr["instructions"]), "names the speaker") {
		t.Errorf("b0_attr instructions = %v, want the bullet and the question only", attr["instructions"])
	}
	if !strings.Contains(fmt.Sprint(criteria["false"]), "names the speaker") || !strings.Contains(fmt.Sprint(criteria["true"]), "according to the source") {
		t.Errorf("b0_attr criteria = %v, want the speaker boundary", attr["criteria"])
	}
	if _, ok := fact["criteria"]; ok {
		t.Errorf("b0_fact criteria = %v, want none", fact["criteria"])
	}
	missing, _ := got.Questions["t1_missing"].(map[string]any)
	if !strings.Contains(fmt.Sprint(missing["instructions"]), "markets moved") || !strings.Contains(fmt.Sprint(missing["instructions"]), "promotional post") {
		t.Errorf("t1_missing = %v, want the second tweet and the promo no", missing)
	}
	if !strings.Contains(got.State, "AI will be powerful") || !strings.Contains(got.State, "big story") {
		t.Errorf("state missing tweet or digest: %s", got.State)
	}
	if scored.Rollup["faithfulness"] != 0.4 || scored.Rollup["coverage"] != 0.55 || scored.Rollup["clarity"] != 0.7 {
		t.Errorf("rollup = %v, want worst defect per dimension", scored.Rollup)
	}
}

func TestJevRollupIsZeroWithoutQuestions(t *testing.T) {
	got := jevRollup(map[string]float64{"split": 0.9, "b0_again": 0.9, "b0_attr": 0.74, "b0_hedge": 0.9})
	if got["faithfulness"] != 0 || got["coverage"] != 0 || got["clarity"] != 0 || got["topicRouting"] != 0 {
		t.Errorf("rollup = %v, want split and attr ignored", got)
	}
	kept := jevRollup(map[string]float64{"b1_merged": 0.7, "b0_fact": 0.4})
	if kept["clarity"] != 0.7 || kept["faithfulness"] != 0.4 {
		t.Errorf("rollup = %v, want merged and fact", kept)
	}
}

type aiBody struct {
	State     string         `json:"state"`
	Questions map[string]any `json:"questions"`
}

func TestJevRoute(t *testing.T) {
	cases := []struct {
		name  string
		score jevScore
		err   string
		want  string
	}{
		{name: "no call", want: ""},
		{name: "error", err: "unavailable", want: "escalate"},
		{
			name: "send",
			score: jevScore{
				Rollup: map[string]float64{"faithfulness": 0.2, "topicRouting": 0.1, "coverage": 0, "clarity": 0.2},
				Detail: map[string]float64{"b0_fact": 0.2, "split": 0.1},
			},
			want: "send",
		},
		{
			name: "repair lists the clear defects in order",
			score: jevScore{
				Rollup: map[string]float64{"faithfulness": 0.91, "topicRouting": 0.1, "coverage": 0.9, "clarity": 0.85},
				Detail: map[string]float64{"b1_cite": 0.91, "b0_fact": 0.4, "split": 0.85, "t2_missing": 0.9},
			},
			want: "repair:b1_cite,t2_missing",
		},
		{
			name: "middle band",
			score: jevScore{
				Rollup: map[string]float64{"faithfulness": 0.5, "topicRouting": 0.1, "coverage": 0, "clarity": 0.1},
				Detail: map[string]float64{"b0_fact": 0.5},
			},
			want: "escalate",
		},
		{
			name: "split does not repair or escalate",
			score: jevScore{
				Rollup: map[string]float64{"faithfulness": 0.1, "topicRouting": 0.1, "coverage": 0.1, "clarity": 0.1},
				Detail: map[string]float64{"b0_again": 0.93, "b0_merged": 0.1},
			},
			want: "send",
		},
		{
			name: "hedge does not repair or escalate",
			score: jevScore{
				Rollup: map[string]float64{"faithfulness": 0.1, "topicRouting": 0.1, "coverage": 0.1, "clarity": 0.1},
				Detail: map[string]float64{"b0_hedge": 0.9, "b0_fact": 0.1},
			},
			want: "send",
		},
		{
			name: "attr does not repair or escalate",
			score: jevScore{
				Rollup: map[string]float64{"faithfulness": 0.1, "topicRouting": 0.1, "coverage": 0.1, "clarity": 0.1},
				Detail: map[string]float64{"b0_attr": 0.74, "b0_fact": 0.1},
			},
			want: "send",
		},
		{
			name: "fact still repairs",
			score: jevScore{
				Rollup: map[string]float64{"faithfulness": 0.85, "topicRouting": 0.1, "coverage": 0, "clarity": 0.1},
				Detail: map[string]float64{"b0_fact": 0.85},
			},
			want: "repair:b0_fact",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := jevRoute(tc.score, tc.err); got != tc.want {
				t.Errorf("jevRoute = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestJevFaithCritiqueUsesFactAndCiteOnly(t *testing.T) {
	digest := "## Crypto\n- Bitcoin fell below $80,000. @kobeissi https://x.com/KobeissiLetter/status/100"
	ignored := jevFaithCritique(digest, map[string]float64{
		"split": 0.93, "b0_attr": 0.90, "b0_section": 0.85, "t0_missing": 0.95, "b0_merged": 0.84,
	})
	if ignored != "" {
		t.Errorf("critique = %q, want empty", ignored)
	}
	got := jevFaithCritique(digest, map[string]float64{"b0_fact": 0.85, "b0_cite": 0.40})
	if !strings.Contains(got, "b0") || !strings.Contains(got, "Bitcoin fell below $80,000") {
		t.Errorf("critique = %q", got)
	}
	kept := []sources.Tweet{
		{ID: "100", Handle: "@kobeissi", Text: "Bitcoin dropped under 80k"},
		{ID: "200", Handle: "@other", Text: "unrelated markets note"},
	}
	tweets := tweetsForFaithRepair(digest, map[string]float64{"b0_fact": 0.85}, kept)
	if len(tweets) != 1 || tweets[0].ID != "100" {
		t.Errorf("tweets = %+v, want the cited tweet only", tweets)
	}
}
