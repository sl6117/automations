package twitterdigest

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/storage"
)

func closeTo(got, want float64) bool {
	return math.Abs(got-want) < 0.0001
}

func handleReport(t *testing.T, s SourcesSummary, handle string) HandleReport {
	t.Helper()
	for _, h := range s.Handles {
		if h.Handle == handle {
			return h
		}
	}
	t.Fatalf("no report row for %q; got %+v", handle, s.Handles)
	return HandleReport{}
}

// Two runs, six posts, 150 billed reads. Every number below is hand-checkable:
// total spend is 150 * $0.005 = $0.75, apportioned by each handle's share of the
// fetched volume, because reads are billed per page regardless of who posted.
func fixtureRuns() []SourceStats {
	return []SourceStats{
		{
			Timestamp: "2026-07-28T09:00:00Z", Source: "xapi", Reads: 100, Fetched: 4, Kept: 2,
			Posts: []PostObservation{
				{ID: "1", Handle: "@a", Likes: 100, Kept: true},
				{ID: "2", Handle: "@a", Likes: 250, Reposts: 50, Kept: true},
				{ID: "3", Handle: "@a", Likes: 50, DropReason: DropLowEngagement},
				{ID: "4", Handle: "@b", Likes: 500, DropReason: DropDuplicate},
			},
		},
		{
			Timestamp: "2026-07-29T09:00:00Z", Source: "xapi", Reads: 50, Fetched: 2, Kept: 2,
			Truncated: true,
			Posts: []PostObservation{
				{ID: "5", Handle: "@a", Likes: 200, Kept: true},
				{ID: "6", Handle: "@c", Likes: 400, Kept: true},
			},
		},
	}
}

func TestSummarizeTotals(t *testing.T) {
	got := Summarize(fixtureRuns())

	if got.Runs != 2 {
		t.Errorf("runs = %d, want 2", got.Runs)
	}
	if got.Fetched != 6 {
		t.Errorf("fetched = %d, want 6", got.Fetched)
	}
	if got.Kept != 4 {
		t.Errorf("kept = %d, want 4", got.Kept)
	}
	if got.Reads != 150 {
		t.Errorf("reads = %d, want 150", got.Reads)
	}
	if !closeTo(got.CostUSD, 0.75) {
		t.Errorf("costUSD = %v, want 0.75", got.CostUSD)
	}
	if got.TruncatedRuns != 1 {
		t.Errorf("truncatedRuns = %d, want 1", got.TruncatedRuns)
	}
	if got.FirstTimestamp != "2026-07-28T09:00:00Z" || got.LastTimestamp != "2026-07-29T09:00:00Z" {
		t.Errorf("range = %s..%s, want the two fixture timestamps", got.FirstTimestamp, got.LastTimestamp)
	}
}

func TestSummarizeRanksHandlesByVolume(t *testing.T) {
	got := Summarize(fixtureRuns())

	if len(got.Handles) != 3 {
		t.Fatalf("got %d handles, want 3", len(got.Handles))
	}
	// @a has 4 of 6 posts; @b and @c tie at 1 and break alphabetically so the
	// report is stable run to run
	want := []string{"@a", "@b", "@c"}
	for i, h := range want {
		if got.Handles[i].Handle != h {
			t.Errorf("handles[%d] = %q, want %q", i, got.Handles[i].Handle, h)
		}
	}
}

func TestSummarizeHandleMath(t *testing.T) {
	got := Summarize(fixtureRuns())

	a := handleReport(t, got, "@a")
	if a.Fetched != 4 || a.Kept != 3 {
		t.Errorf("@a fetched/kept = %d/%d, want 4/3", a.Fetched, a.Kept)
	}
	if !closeTo(a.VolumeShare, 4.0/6.0) {
		t.Errorf("@a volumeShare = %v, want %v", a.VolumeShare, 4.0/6.0)
	}
	if !closeTo(a.KeepRate, 0.75) {
		t.Errorf("@a keepRate = %v, want 0.75", a.KeepRate)
	}
	// $0.75 total * 4/6 of the volume = $0.50, over 3 kept posts
	if !closeTo(a.CostUSD, 0.50) {
		t.Errorf("@a costUSD = %v, want 0.50", a.CostUSD)
	}
	if !closeTo(a.CostPerKeptUSD, 0.50/3.0) {
		t.Errorf("@a costPerKeptUSD = %v, want %v", a.CostPerKeptUSD, 0.50/3.0)
	}
	// engagement is likes+reposts over every fetched post: 100, 300, 50, 200
	if a.MaxEngagement != 300 {
		t.Errorf("@a maxEngagement = %d, want 300", a.MaxEngagement)
	}
	if a.MedianEngagement != 150 {
		t.Errorf("@a medianEngagement = %d, want 150", a.MedianEngagement)
	}
}

// A handle that never survives the filter is the whole point of the report: it must
// show up as pure cost rather than being hidden by a division by zero.
func TestSummarizeHandleThatKeepsNothing(t *testing.T) {
	got := Summarize(fixtureRuns())

	b := handleReport(t, got, "@b")
	if b.Kept != 0 {
		t.Fatalf("@b kept = %d, want 0", b.Kept)
	}
	if !closeTo(b.KeepRate, 0) {
		t.Errorf("@b keepRate = %v, want 0", b.KeepRate)
	}
	if !closeTo(b.CostUSD, 0.125) {
		t.Errorf("@b costUSD = %v, want 0.125", b.CostUSD)
	}
	if b.CostPerKeptUSD != 0 {
		t.Errorf("@b costPerKeptUSD = %v, want 0 (undefined, not infinity)", b.CostPerKeptUSD)
	}
	if math.IsInf(b.CostPerKeptUSD, 0) || math.IsNaN(b.CostPerKeptUSD) {
		t.Error("@b costPerKeptUSD must never be Inf or NaN")
	}
	if b.DroppedDuplicate != 1 {
		t.Errorf("@b droppedDuplicate = %d, want 1", b.DroppedDuplicate)
	}
}

func TestSummarizeCountsDropReasons(t *testing.T) {
	got := Summarize(fixtureRuns())

	a := handleReport(t, got, "@a")
	if a.DroppedLowEngagement != 1 {
		t.Errorf("@a droppedLowEngagement = %d, want 1", a.DroppedLowEngagement)
	}
	if a.DroppedDuplicate != 0 || a.DroppedPerAuthorCap != 0 {
		t.Errorf("@a unexpected drops: %+v", a)
	}
}

// Dev runs against the mock source would otherwise skew every ratio in the report.
func TestSummarizeSkipsNonXAPIRuns(t *testing.T) {
	runs := append(fixtureRuns(), SourceStats{
		Timestamp: "2026-07-30T09:00:00Z", Source: "mock", Reads: 0, Fetched: 2, Kept: 2,
		Posts: []PostObservation{
			{ID: "90", Handle: "@fake", Likes: 999, Kept: true},
			{ID: "91", Handle: "@a", Likes: 999, Kept: true},
		},
	})

	got := Summarize(runs)

	if got.Runs != 2 {
		t.Errorf("runs = %d, want 2 (the mock run is excluded)", got.Runs)
	}
	if got.SkippedRuns != 1 {
		t.Errorf("skippedRuns = %d, want 1", got.SkippedRuns)
	}
	if got.Fetched != 6 {
		t.Errorf("fetched = %d, want 6", got.Fetched)
	}
	for _, h := range got.Handles {
		if h.Handle == "@fake" {
			t.Error("@fake leaked in from the mock run")
		}
	}
	if a := handleReport(t, got, "@a"); a.Fetched != 4 {
		t.Errorf("@a fetched = %d, want 4: mock posts must not inflate a real handle", a.Fetched)
	}
}

func TestSummarizeEmptyInput(t *testing.T) {
	got := Summarize(nil)

	if got.Runs != 0 || got.Fetched != 0 || got.Kept != 0 {
		t.Errorf("empty summary should be zero: %+v", got)
	}
	if len(got.Handles) != 0 {
		t.Errorf("handles = %v, want none", got.Handles)
	}
	if math.IsNaN(got.CostUSD) {
		t.Error("costUSD is NaN on empty input")
	}
}

func TestLoadSourceStats(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}
	ctx := context.Background()

	for _, run := range fixtureRuns() {
		data, err := json.Marshal(run)
		if err != nil {
			t.Fatal(err)
		}
		key := "logs/sourcestats/" + strings.ReplaceAll(run.Timestamp, ":", "-") + "-twitter-digest.json"
		if err := store.Put(ctx, key, data); err != nil {
			t.Fatal(err)
		}
	}

	got, err := LoadSourceStats(ctx, store, "")
	if err != nil {
		t.Fatalf("LoadSourceStats: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d runs, want 2", len(got))
	}
	if got[0].Timestamp != "2026-07-28T09:00:00Z" {
		t.Errorf("runs should come back oldest first, got %q", got[0].Timestamp)
	}
	if len(got[0].Posts) != 4 {
		t.Errorf("posts did not survive the round trip: %+v", got[0])
	}
}

func TestLoadSourceStatsSince(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}
	ctx := context.Background()

	for _, run := range fixtureRuns() {
		data, err := json.Marshal(run)
		if err != nil {
			t.Fatal(err)
		}
		key := "logs/sourcestats/" + strings.ReplaceAll(run.Timestamp, ":", "-") + "-twitter-digest.json"
		if err := store.Put(ctx, key, data); err != nil {
			t.Fatal(err)
		}
	}

	got, err := LoadSourceStats(ctx, store, "2026-07-29")
	if err != nil {
		t.Fatalf("LoadSourceStats: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d runs, want 1", len(got))
	}
	if got[0].Timestamp != "2026-07-29T09:00:00Z" {
		t.Errorf("kept the wrong run: %q", got[0].Timestamp)
	}
}

func TestLoadSourceStatsNoDataYet(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}

	got, err := LoadSourceStats(context.Background(), store, "")
	if err != nil {
		t.Fatalf("an empty archive is not an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d runs, want 0", len(got))
	}
}

func TestSourcesReportOutput(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}
	ctx := context.Background()

	for _, run := range fixtureRuns() {
		data, err := json.Marshal(run)
		if err != nil {
			t.Fatal(err)
		}
		key := "logs/sourcestats/" + strings.ReplaceAll(run.Timestamp, ":", "-") + "-twitter-digest.json"
		if err := store.Put(ctx, key, data); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	if err := SourcesReport(ctx, store, &buf, ""); err != nil {
		t.Fatalf("SourcesReport: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"@a", "@b", "@c"} {
		if !strings.Contains(out, want) {
			t.Errorf("report is missing %q:\n%s", want, out)
		}
	}
	// the truncation count is the headline finding of the measurement week
	if !strings.Contains(strings.ToLower(out), "cap") {
		t.Errorf("report does not mention the page cap:\n%s", out)
	}
}

func TestSourcesReportWithNoData(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}

	var buf bytes.Buffer
	if err := SourcesReport(context.Background(), store, &buf, ""); err != nil {
		t.Fatalf("no data is not an error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("report should say something when there is no data yet")
	}
}

func TestMedian(t *testing.T) {
	cases := []struct {
		in   []int
		want int
	}{
		{nil, 0},
		{[]int{5}, 5},
		{[]int{3, 1}, 2},
		{[]int{5, 1, 3}, 3},
		{[]int{4, 1, 3, 2}, 2},
	}
	for _, c := range cases {
		if got := median(c.in); got != c.want {
			t.Errorf("median(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestMedianDoesNotMutateInput(t *testing.T) {
	in := []int{9, 1, 5}
	median(in)
	if in[0] != 9 || in[1] != 1 || in[2] != 5 {
		t.Errorf("median reordered its input: %v", in)
	}
}
