package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/obs"
	"github.com/sl6117/automations/internal/storage"
	"github.com/sl6117/automations/pkg/sources"
	twitterdigest "github.com/sl6117/automations/projects/twitter-digest"
)

func seedArtifact(t *testing.T, store storage.Store, key string, a twitterdigest.Artifact) {
	t.Helper()
	data, err := json.Marshal(a)

	if err != nil {
		t.Fatal(err)
	}

	if err := store.Put(context.Background(), key, data); err != nil {
		t.Fatal(err)
	}
}

func TestGetArtifactOmitsTweetsByDefault(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}

	s := &digestServer{store: store}
	key := "logs/runs/2026-07-21T16-00-26Z-twitter-digest-english.json"

	seedArtifact(t, store, key, twitterdigest.Artifact{
		Language: "English",
		Digest:   "## AI\n- story",
		Kept:     []sources.Tweet{{Author: "Dario", Handle: "@d", Text: "AI", URL: "https://x.com/i/1"}},
	})

	_, out, err := s.getArtifact(context.Background(), nil, getArtifactInput{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	if out.Artifact.Kept != nil {
		t.Errorf("Kept = %v, want nil without includeTweets", out.Artifact.Kept)
	}
	if !strings.Contains(out.Artifact.Digest, "## AI") {
		t.Errorf("Digest = %q, want the seeded digest", out.Artifact.Digest)
	}

	_, out, err = s.getArtifact(context.Background(), nil, getArtifactInput{Key: key, IncludeTweets: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Artifact.Kept) != 1 {
		t.Errorf("Kept length = %d, want 1 with includeTweets", len(out.Artifact.Kept))
	}
}

func TestGetVerdictsSummarizesRuns(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}
	s := &digestServer{store: store}

	seedArtifact(t, store, "logs/runs/2026-07-20T16-00-00Z-twitter-digest-russian.json", twitterdigest.Artifact{
		Language: "Russian",
		Judge: &twitterdigest.JudgeReport{
			Faithfulness: twitterdigest.Verdict{Pass: false, Reason: "added a year"},
			TopicRouting: twitterdigest.Verdict{Pass: true},
			Coverage:     twitterdigest.Verdict{Pass: true},
			Clarity:      twitterdigest.Verdict{Pass: true},
		},
	})
	seedArtifact(t, store, "logs/runs/2026-07-21T16-00-00Z-twitter-digest-english.json", twitterdigest.Artifact{
		Language: "English",
	})

	_, out, err := s.getVerdicts(context.Background(), nil, getVerdictsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Verdicts) != 2 {
		t.Fatalf("verdicts = %d, want 2", len(out.Verdicts))
	}
	if out.Verdicts[0].Pass || len(out.Verdicts[0].Failures) != 1 || !strings.Contains(out.Verdicts[0].Failures[0], "faithfulness") {
		t.Errorf("russian row = %+v, want one faithfulness failure", out.Verdicts[0])
	}
	if out.Verdicts[1].Judged {
		t.Errorf("english row = %+v, want Judged false", out.Verdicts[1])
	}
}

func TestGetCostAggregatesByMonth(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}
	s := &digestServer{store: store}
	for _, run := range []obs.Run{
		{Timestamp: "2026-07-20T16:00:00Z", InputTokens: 100, OutputTokens: 50, SourceReads: 150, CostUSD: 0.01},
		{Timestamp: "2026-07-21T16:00:00Z", InputTokens: 200, OutputTokens: 100, SourceReads: 100, CostUSD: 0.02},
		{Timestamp: "2026-06-01T16:00:00Z", InputTokens: 999, OutputTokens: 1, SourceReads: 0, CostUSD: 0.99},
	} {
		line, err := json.Marshal(run)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Append(context.Background(), obs.CostLogKey, line); err != nil {
			t.Fatal(err)
		}
	}
	_, out, err := s.getCost(context.Background(), nil, getCostInput{Month: "2026-07"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Months) != 1 {
		t.Fatalf("months = %+v, want just 2026-07", out.Months)
	}
	m := out.Months[0]
	if m.Runs != 2 || m.Tokens != 450 || m.SourceReads != 250 || m.CostUSD != 0.03 {
		t.Errorf("2026-07 = %+v, want runs 2, tokens 450, reads 250, cost 0.03", m)
	}
}
