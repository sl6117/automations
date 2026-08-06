package twitterdigest

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sl6117/automations/internal/storage"
)

func TestBacktestRankedFilterCiteRecall(t *testing.T) {
	day := func(d, h int) time.Time {
		return time.Date(2026, 8, d, h, 0, 0, 0, time.UTC)
	}
	// Prior day builds @wire median around 45 so day's 50 clears relative rank.
	prior := SourceStats{
		Timestamp: day(4, 9).Format(time.RFC3339), Source: "xapi",
		Posts: []PostObservation{
			{ID: "p1", Handle: "@wire", Likes: 40, OwnTextLength: 20},
			{ID: "p2", Handle: "@wire", Likes: 50, OwnTextLength: 20},
		},
	}
	// Production dropped the wire post (low_engagement); kept the loud one.
	// IDs are numeric: parseCitations only extracts /status/(\d+).
	run := SourceStats{
		Timestamp: day(5, 9).Format(time.RFC3339), Source: "xapi",
		Posts: []PostObservation{
			{
				ID: "1001", Handle: "@wire", Likes: 50, OwnTextLength: 20,
				CreatedAt: day(5, 7), Kept: false, DropReason: DropLowEngagement,
			},
			{
				ID: "1002", Handle: "@loud", Likes: 500, OwnTextLength: 20,
				AuthorFollowers: 1_000_000, CreatedAt: day(5, 7), Kept: true,
			},
			{
				ID: "1003", Handle: "@loud", Likes: 200, OwnTextLength: 20,
				AuthorFollowers: 1_000_000, CreatedAt: day(5, 7), Kept: true,
			},
		},
	}
	artifacts := []Artifact{
		{
			Timestamp: day(5, 16).Format(time.RFC3339),
			Language:  "English",
			Digest:    "(@wire https://x.com/wire/status/1001) (@loud https://x.com/loud/status/1002)",
		},
	}

	got := backtestRankedFilter([]SourceStats{prior, run}, artifacts, BacktestParams{
		Lookback:      7 * 24 * time.Hour,
		MinEngagement: 100,
		RelativeK:     0.5,
		MaxPerAuthor:  3,
		DigestBudget:  32,
		Alpha:         0.7,
	})

	if got.Runs != 2 {
		t.Fatalf("Runs = %d, want 2 xapi runs scored", got.Runs)
	}
	if got.Cited != 2 {
		t.Fatalf("Cited = %d, want 2", got.Cited)
	}
	// Baseline kept only 1002 among the two cited ids.
	if got.BaselineCiteRecall != 0.5 {
		t.Fatalf("BaselineCiteRecall = %v, want 0.5", got.BaselineCiteRecall)
	}
	// Ranked should recover 1001 → both cited ids kept.
	if got.RankedCiteRecall != 1.0 {
		t.Fatalf("RankedCiteRecall = %v, want 1.0", got.RankedCiteRecall)
	}
	if got.NewswireRecovered < 1 {
		t.Fatalf("NewswireRecovered = %d, want >= 1", got.NewswireRecovered)
	}
}

func TestBacktestRankedFilterIgnoresMockAndEmptyDigests(t *testing.T) {
	ts := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	runs := []SourceStats{{
		Timestamp: ts.Format(time.RFC3339), Source: "mock",
		Posts: []PostObservation{{ID: "1", Handle: "@a", Likes: 10, OwnTextLength: 5, Kept: true}},
	}}
	artifacts := []Artifact{{
		Timestamp: ts.Format(time.RFC3339), Language: "English", Digest: "   ",
	}}
	got := backtestRankedFilter(runs, artifacts, BacktestParams{
		Lookback: 7 * 24 * time.Hour, MinEngagement: 100, RelativeK: 0.5,
		MaxPerAuthor: 3, DigestBudget: 32, Alpha: 0.7,
	})
	if got.Runs != 0 || got.Cited != 0 {
		t.Fatalf("got %+v, want empty backtest over mock/empty", got)
	}
}

func TestBacktestRankedFilterRTKeepDoesNotRiseOnFixture(t *testing.T) {
	ts := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	prior := SourceStats{
		Timestamp: ts.Add(-24 * time.Hour).Format(time.RFC3339), Source: "xapi",
		Posts: []PostObservation{
			{ID: "hist", Handle: "@a", Likes: 20, OwnTextLength: 20},
		},
	}
	run := SourceStats{
		Timestamp: ts.Format(time.RFC3339), Source: "xapi",
		Posts: []PostObservation{
			{
				ID: "rt", Handle: "@a", Likes: 0, Reposts: 5000, IsRetweet: true,
				OwnTextLength: 0, CreatedAt: ts, Kept: true, // baseline wrongly kept RT
			},
			{
				ID: "own1", Handle: "@a", Likes: 30, OwnTextLength: 20, CreatedAt: ts, Kept: true,
			},
			{
				ID: "own2", Handle: "@a", Likes: 25, OwnTextLength: 20, CreatedAt: ts, Kept: false,
				DropReason: DropPerAuthorCap,
			},
		},
	}
	got := backtestRankedFilter([]SourceStats{prior, run}, nil, BacktestParams{
		Lookback: 7 * 24 * time.Hour, MinEngagement: 100, RelativeK: 0.5,
		MaxPerAuthor: 2, DigestBudget: 32, Alpha: 0.7,
	})
	if got.RankedRTKept > got.BaselineRTKept {
		t.Fatalf("RT kept rose: baseline=%d ranked=%d", got.BaselineRTKept, got.RankedRTKept)
	}
}

func TestWriteFilterBacktest(t *testing.T) {
	var buf bytes.Buffer
	writeFilterBacktest(&buf, FilterBacktest{
		Runs: 3, BaselineKept: 10, RankedKept: 12,
		Cited: 4, BaselineCitedKept: 2, RankedCitedKept: 3,
		BaselineCiteRecall: 0.5, RankedCiteRecall: 0.75,
		BaselineRTKept: 2, RankedRTKept: 1, NewswireRecovered: 5,
	}, BacktestParams{
		Lookback: 7 * 24 * time.Hour, MinEngagement: 100, RelativeK: 0.5,
		MaxPerAuthor: 3, DigestBudget: 32, Alpha: 0.7,
	})
	got := buf.String()
	for _, want := range []string{
		"rank-budget backtest",
		"minEngagement=100",
		"relativeK=0.50",
		"cite recall: baseline 50.0% → ranked 75.0%",
		"newswire recovered: 5",
		"RT kept: baseline 2 → ranked 1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q\n%s", want, got)
		}
	}
}

func TestRankBacktestReport(t *testing.T) {
	t.Setenv("AUTOMATION_ROOT", t.TempDir())
	store := &storage.FS{Root: t.TempDir()}
	ctx := context.Background()

	day := func(d int) time.Time {
		return time.Date(2026, 8, d, 9, 0, 0, 0, time.UTC)
	}
	runs := []SourceStats{
		{
			Timestamp: day(4).Format(time.RFC3339), Source: "xapi",
			Posts: []PostObservation{
				{ID: "1", Handle: "@wire", Likes: 40, OwnTextLength: 20},
				{ID: "2", Handle: "@wire", Likes: 50, OwnTextLength: 20},
			},
		},
		{
			Timestamp: day(5).Format(time.RFC3339), Source: "xapi",
			Posts: []PostObservation{
				{
					ID: "1001", Handle: "@wire", Likes: 50, OwnTextLength: 20,
					CreatedAt: day(5).Add(-2 * time.Hour), Kept: false, DropReason: DropLowEngagement,
				},
				{
					ID: "1002", Handle: "@loud", Likes: 500, OwnTextLength: 20,
					AuthorFollowers: 1_000_000, CreatedAt: day(5).Add(-2 * time.Hour), Kept: true,
				},
			},
		},
	}
	for _, run := range runs {
		data, err := json.Marshal(run)
		if err != nil {
			t.Fatal(err)
		}
		key := "logs/sourcestats/" + strings.ReplaceAll(run.Timestamp, ":", "-") + "-twitter-digest.json"
		if err := store.Put(ctx, key, data); err != nil {
			t.Fatal(err)
		}
	}
	art := Artifact{
		Timestamp: day(5).Add(7 * time.Hour).Format(time.RFC3339),
		Language:  "English",
		Digest:    "(@wire https://x.com/wire/status/1001) (@loud https://x.com/loud/status/1002)",
	}
	adata, err := json.Marshal(art)
	if err != nil {
		t.Fatal(err)
	}
	akey := "logs/runs/" + strings.ReplaceAll(art.Timestamp, ":", "-") + "-twitter-digest-english.json"
	if err := store.Put(ctx, akey, adata); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RankBacktestReport(ctx, store, &buf, "", BacktestParams{
		Lookback: 7 * 24 * time.Hour, MinEngagement: 100, RelativeK: 0.5,
		MaxPerAuthor: 3, DigestBudget: 32, Alpha: 0.7,
	}); err != nil {
		t.Fatalf("RankBacktestReport: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "cite recall: baseline 50.0% → ranked 100.0%") {
		t.Fatalf("unexpected report:\n%s", got)
	}
	if !strings.Contains(got, "newswire recovered: 1") {
		t.Fatalf("expected newswire recovery, got:\n%s", got)
	}
}
