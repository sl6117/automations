package twitterdigest

import (
	"testing"
	"time"
)

func hybridParams(runAt time.Time) RankParams {
	return RankParams{
		MinEngagement: 100,
		RelativeK:     0.5,
		MaxPerAuthor:  3,
		DigestBudget:  32,
		Alpha:         0.7,
		RunAt:         runAt,
	}
}

func TestRankScoreColdStartUsesFollowerScore(t *testing.T) {
	runAt := time.Date(2026, 8, 5, 16, 0, 0, 0, time.UTC)
	p := PostObservation{
		Likes: 100, Reposts: 0, AuthorFollowers: 1000,
		CreatedAt: runAt.Add(-2 * time.Hour),
	}
	got := rankScore(p, 0, runAt, 0.7)
	want := p.score()
	if got != want {
		t.Fatalf("cold-start rankScore = %v, want %v (eng/followers)", got, want)
	}
}

func TestRankScoreRelativeBeatsAbsoluteWhenMedianHigh(t *testing.T) {
	runAt := time.Date(2026, 8, 5, 16, 0, 0, 0, time.UTC)
	p := PostObservation{
		Likes: 50, Reposts: 0, AuthorFollowers: 10_000_000,
		CreatedAt: runAt.Add(-2 * time.Hour),
	}
	got := rankScore(p, 40, runAt, 1.0)
	if got < 1.0 {
		t.Fatalf("relative score = %v, want >= 1.0 (50/40)", got)
	}
}

func TestRankScoreFresherBeatsOlderAtSameEngagement(t *testing.T) {
	runAt := time.Date(2026, 8, 5, 16, 0, 0, 0, time.UTC)
	fresh := PostObservation{Likes: 100, CreatedAt: runAt.Add(-1 * time.Hour)}
	stale := PostObservation{Likes: 100, CreatedAt: runAt.Add(-10 * time.Hour)}
	aFresh := rankScore(fresh, 100, runAt, 0.0)
	aStale := rankScore(stale, 100, runAt, 0.0)
	if aFresh <= aStale {
		t.Fatalf("fresh=%v stale=%v; fresher post should rank higher on eph", aFresh, aStale)
	}
}

func TestHandleMediansRollingWindowExcludesRTsAndFuture(t *testing.T) {
	day := func(d int) time.Time {
		return time.Date(2026, 8, d, 9, 0, 0, 0, time.UTC)
	}
	prior := []SourceStats{
		{
			Timestamp: day(1).Format(time.RFC3339), Source: "xapi",
			Posts: []PostObservation{
				{Handle: "@wire", Likes: 40, IsRetweet: false},
				{Handle: "@wire", Likes: 60, IsRetweet: false},
				{Handle: "@wire", Likes: 999, IsRetweet: true},
			},
		},
		{
			Timestamp: day(10).Format(time.RFC3339), Source: "xapi",
			Posts: []PostObservation{
				{Handle: "@wire", Likes: 1, IsRetweet: false},
			},
		},
		{
			Timestamp: day(2).Format(time.RFC3339), Source: "mock",
			Posts: []PostObservation{
				{Handle: "@wire", Likes: 2, IsRetweet: false},
			},
		},
	}
	got := handleMedians(prior, 7*24*time.Hour, day(5))
	if got["@wire"] != 50 {
		t.Fatalf("@wire median = %d, want 50", got["@wire"])
	}
}

func TestClearsHybridFloor(t *testing.T) {
	if !clearsHybridFloor(150, false, 40, 100, 0.5) {
		t.Fatal("eng>=min should clear")
	}
	if !clearsHybridFloor(50, false, 80, 100, 0.5) {
		t.Fatal("eng>=k*median should clear")
	}
	if clearsHybridFloor(30, false, 80, 100, 0.5) {
		t.Fatal("below both floors should drop")
	}
	if clearsHybridFloor(50, true, 40, 100, 0.5) {
		t.Fatal("RT must not clear via relative")
	}
	if !clearsHybridFloor(150, true, 40, 100, 0.5) {
		t.Fatal("RT may clear via absolute")
	}
}

func TestAddOnlyPreservesBaselineAndFillsSpare(t *testing.T) {
	runAt := time.Date(2026, 8, 5, 16, 0, 0, 0, time.UTC)
	obs := []PostObservation{
		{ID: "cited", Handle: "@loud", Likes: 200, OwnTextLength: 20, Kept: true, AuthorFollowers: 1000},
		{ID: "wire", Handle: "@wire", Likes: 50, OwnTextLength: 20, Kept: false, DropReason: DropLowEngagement},
	}
	p := hybridParams(runAt)
	p.DigestBudget = 2
	out := applyAddOnly(obs, map[string]int{"@wire": 40}, p)
	kept := keptIDs(out)
	if !containsID(kept, "cited") || !containsID(kept, "wire") {
		t.Fatalf("spare budget should keep baseline + relative; kept=%v", kept)
	}
}

func TestAddOnlyNoSpareDoesNotDisplace(t *testing.T) {
	runAt := time.Date(2026, 8, 5, 16, 0, 0, 0, time.UTC)
	obs := []PostObservation{
		{ID: "cited", Handle: "@loud", Likes: 200, OwnTextLength: 20, Kept: true},
		{ID: "wire", Handle: "@wire", Likes: 50, OwnTextLength: 20, Kept: false, DropReason: DropLowEngagement},
	}
	p := hybridParams(runAt)
	p.DigestBudget = 1
	out := applyAddOnly(obs, map[string]int{"@wire": 40}, p)
	kept := keptIDs(out)
	if !containsID(kept, "cited") || containsID(kept, "wire") {
		t.Fatalf("full budget must not displace baseline; kept=%v", kept)
	}
}

func TestApplyRankedFilterDropsBelowRelativeFloor(t *testing.T) {
	runAt := time.Date(2026, 8, 5, 16, 0, 0, 0, time.UTC)
	obs := []PostObservation{
		{ID: "weak", Handle: "@wire", Likes: 10, OwnTextLength: 20, CreatedAt: runAt},
		{ID: "ok", Handle: "@wire", Likes: 50, OwnTextLength: 20, CreatedAt: runAt},
	}
	out := applyRankedFilter(obs, map[string]int{"@wire": 80}, hybridParams(runAt))
	for _, o := range out {
		if o.ID == "weak" && (o.Kept || o.DropReason != DropLowEngagement) {
			t.Fatalf("weak: kept=%v reason=%q", o.Kept, o.DropReason)
		}
		if o.ID == "ok" && !o.Kept {
			t.Fatalf("ok should enter via relative spare path: %+v", o)
		}
	}
}

func TestApplyRankedFilterDropsEmptyOwnText(t *testing.T) {
	runAt := time.Date(2026, 8, 5, 16, 0, 0, 0, time.UTC)
	obs := []PostObservation{
		{ID: "empty", Handle: "@a", Likes: 200, OwnTextLength: 0, IsRetweet: false, CreatedAt: runAt},
		{ID: "ok", Handle: "@a", Likes: 150, OwnTextLength: 30, CreatedAt: runAt},
	}
	out := applyRankedFilter(obs, map[string]int{"@a": 40}, hybridParams(runAt))
	for _, o := range out {
		if o.ID == "empty" && (o.Kept || o.DropReason != DropEmptyOwnText) {
			t.Fatalf("empty own text: kept=%v reason=%q", o.Kept, o.DropReason)
		}
		if o.ID == "ok" && !o.Kept {
			t.Fatalf("substance post dropped: %+v", o)
		}
	}
}

func TestApplyRankedFilterPerAuthorCapDemotesRetweets(t *testing.T) {
	runAt := time.Date(2026, 8, 5, 16, 0, 0, 0, time.UTC)
	obs := []PostObservation{
		{ID: "rt", Handle: "@a", Likes: 0, Reposts: 9000, IsRetweet: true, OwnTextLength: 0, CreatedAt: runAt},
		{ID: "own1", Handle: "@a", Likes: 150, OwnTextLength: 20, CreatedAt: runAt},
		{ID: "own2", Handle: "@a", Likes: 120, OwnTextLength: 20, CreatedAt: runAt},
	}
	p := hybridParams(runAt)
	p.MaxPerAuthor = 2
	out := applyRankedFilter(obs, map[string]int{"@a": 5}, p)
	kept := keptIDs(out)
	if containsID(kept, "rt") {
		t.Fatalf("retweet took a per-author slot: kept=%v", kept)
	}
	if len(kept) != 2 {
		t.Fatalf("kept=%v, want both own posts", kept)
	}
}

func TestApplyRankedFilterDigestBudget(t *testing.T) {
	runAt := time.Date(2026, 8, 5, 16, 0, 0, 0, time.UTC)
	obs := []PostObservation{
		{ID: "hi", Handle: "@a", Likes: 200, OwnTextLength: 20, CreatedAt: runAt},
		{ID: "mid", Handle: "@b", Likes: 150, OwnTextLength: 20, CreatedAt: runAt},
		{ID: "lo", Handle: "@c", Likes: 120, OwnTextLength: 20, CreatedAt: runAt},
	}
	p := hybridParams(runAt)
	p.DigestBudget = 2
	out := applyRankedFilter(obs, map[string]int{"@a": 100, "@b": 100, "@c": 100}, p)
	kept := keptIDs(out)
	if len(kept) != 2 || containsID(kept, "lo") {
		t.Fatalf("digestBudget=2 should keep hi+mid, got %v", kept)
	}
	for _, o := range out {
		if o.ID == "lo" && o.DropReason != DropDigestBudget {
			t.Fatalf("lo drop reason = %q, want %q", o.DropReason, DropDigestBudget)
		}
	}
}

func keptIDs(obs []PostObservation) []string {
	var ids []string
	for _, o := range obs {
		if o.Kept {
			ids = append(ids, o.ID)
		}
	}
	return ids
}

func containsID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}
