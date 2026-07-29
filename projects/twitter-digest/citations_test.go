package twitterdigest

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/storage"
	"github.com/sl6117/automations/pkg/sources"
)

// citationFixture is one measurement day published twice (English + Korean) plus a
// second day published once. Every number in the tests below is hand-checkable:
//
//	day 1 fed posts 1,2 (@a) and 3 (@b); the English digest cited 1 and 3, Korean cited 1
//	day 2 fed posts 4 (@a) and 5 (@c); the digest cited 4
//
// so across the archive @a is 2 of 3, @b is 1 of 1, @c is 0 of 1, and the archive as a
// whole is 3 of 5. The English/Korean pair carries the SAME kept posts, which is the
// trap this fixture exists to catch: summing instead of unioning double-counts day 1.
func citationFixture() []Artifact {
	day1 := []sources.Tweet{
		{ID: "1", Handle: "@a", URL: "https://x.com/a/status/1"},
		{ID: "2", Handle: "@a", URL: "https://x.com/a/status/2"},
		{ID: "3", Handle: "@b", URL: "https://x.com/b/status/3"},
	}
	day2 := []sources.Tweet{
		{ID: "4", Handle: "@a", URL: "https://x.com/a/status/4"},
		{ID: "5", Handle: "@c", URL: "https://x.com/c/status/5"},
	}
	return []Artifact{
		{
			Timestamp: "2026-07-04T16:00:00Z", Language: "English", Kept: day1,
			Digest: "## World News\n\n- A thing happened. (@a https://x.com/a/status/1)\n- Another. (@b https://x.com/b/status/3)\n",
		},
		{
			Timestamp: "2026-07-04T16:00:02Z", Language: "Korean", Kept: day1,
			Digest: "## World News\n\n- 어떤 일이 있었다. (@a https://x.com/a/status/1)\n",
		},
		{
			Timestamp: "2026-07-05T16:00:00Z", Language: "English", Kept: day2,
			Digest: "## Econ\n\n- Markets moved. (@a https://x.com/a/status/4)\n",
		},
	}
}

func TestParseCitations(t *testing.T) {
	cases := []struct {
		name   string
		digest string
		want   []string
	}{
		{"empty", "", nil},
		{"no citations", "## World News\n\n- Nothing to link here.\n", nil},
		{
			"one bullet, two merged citations",
			"- Merged story. (@a https://x.com/a/status/111, @b https://x.com/b/status/222)\n",
			[]string{"111", "222"},
		},
		{
			// the most significant story carries a trailing bold clause; the citation
			// still has to come out cleanly
			"citation followed by the why-it-matters clause",
			"- Big one. (@a https://x.com/a/status/333) **This matters because reasons.**\n",
			[]string{"333"},
		},
		{
			// the prompt says cite each URL at most once, but the rate must not depend
			// on the model obeying that
			"repeated citation counts once",
			"- One. (@a https://x.com/a/status/444)\n- Two. (@a https://x.com/a/status/444)\n",
			[]string{"444"},
		},
		{
			"twitter.com host still matches",
			"- Old host. (@a https://twitter.com/a/status/555)\n",
			[]string{"555"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseCitations(c.digest)
			if len(got) != len(c.want) {
				t.Fatalf("parseCitations(%q) = %v, want %v", c.digest, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("id[%d] = %q, want %q (order of first appearance)", i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestSummarizeCitationsTotals(t *testing.T) {
	got := SummarizeCitations(citationFixture())

	if got.Digests != 3 {
		t.Errorf("digests = %d, want 3", got.Digests)
	}
	// 5 distinct posts reached the model, not 8: day 1 was published twice
	if got.Kept != 5 {
		t.Errorf("kept = %d, want 5 (the English/Korean pair must not double-count)", got.Kept)
	}
	if got.Cited != 3 {
		t.Errorf("cited = %d, want 3 (posts 1, 3, 4)", got.Cited)
	}
	if !closeTo(got.CiteRate, 3.0/5.0) {
		t.Errorf("citeRate = %v, want %v", got.CiteRate, 3.0/5.0)
	}
	if got.FirstTimestamp != "2026-07-04T16:00:00Z" || got.LastTimestamp != "2026-07-05T16:00:00Z" {
		t.Errorf("window = %s..%s, want the fixture's first and last", got.FirstTimestamp, got.LastTimestamp)
	}
	if got.UnmatchedCitations != 0 {
		t.Errorf("unmatchedCitations = %d, want 0: every cited URL is one we fed it", got.UnmatchedCitations)
	}
}

func TestSummarizeCitationsPerHandle(t *testing.T) {
	got := SummarizeCitations(citationFixture())

	if len(got.ByHandle) != 3 {
		t.Fatalf("got %d handles, want 3: %+v", len(got.ByHandle), got.ByHandle)
	}
	cases := []struct {
		handle string
		kept   int
		cited  int
		rate   float64
	}{
		// posts 1, 2, 4 fed; 1 and 4 cited. Post 2 is the interesting one: it cleared
		// the filter, cost a read, reached the model, and the editor passed on it.
		{"@a", 3, 2, 2.0 / 3.0},
		// cited only in the English digest: the union across languages must count it
		{"@b", 1, 1, 1},
		// reached the model and was ignored, which is a real signal, unlike a handle
		// that never got there at all
		{"@c", 1, 0, 0},
	}
	for _, c := range cases {
		r, ok := got.ByHandle[c.handle]
		if !ok {
			t.Errorf("no row for %s", c.handle)
			continue
		}
		if r.Handle != c.handle {
			t.Errorf("%s: Handle field = %q", c.handle, r.Handle)
		}
		if r.Kept != c.kept || r.Cited != c.cited {
			t.Errorf("%s kept/cited = %d/%d, want %d/%d", c.handle, r.Kept, r.Cited, c.kept, c.cited)
		}
		if !closeTo(r.CiteRate, c.rate) {
			t.Errorf("%s citeRate = %v, want %v", c.handle, r.CiteRate, c.rate)
		}
	}
}

// A run that failed before publishing exercised no editorial judgment. Counting its
// kept posts as uncited would silently punish whichever handles happened to be in it.
func TestSummarizeCitationsSkipsEmptyDigests(t *testing.T) {
	artifacts := append(citationFixture(), Artifact{
		Timestamp: "2026-07-06T16:00:00Z", Language: "English", Digest: "   \n",
		Kept: []sources.Tweet{{ID: "9", Handle: "@d", URL: "https://x.com/d/status/9"}},
	})

	got := SummarizeCitations(artifacts)

	if got.Digests != 3 {
		t.Errorf("digests = %d, want 3 (the empty one is not a digest)", got.Digests)
	}
	if got.SkippedDigests != 1 {
		t.Errorf("skippedDigests = %d, want 1", got.SkippedDigests)
	}
	if got.Kept != 5 {
		t.Errorf("kept = %d, want 5: the failed run's posts must not enter the denominator", got.Kept)
	}
	if _, ok := got.ByHandle["@d"]; ok {
		t.Error("@d appears with a 0% rate from a run that never published")
	}
}

// A cited URL that was never in any kept set means the model invented it. Free
// hallucination canary: worth surfacing, not worth failing the report over.
func TestSummarizeCitationsCountsUnmatchedCitations(t *testing.T) {
	got := SummarizeCitations([]Artifact{{
		Timestamp: "2026-07-04T16:00:00Z", Language: "English",
		Kept:   []sources.Tweet{{ID: "1", Handle: "@a", URL: "https://x.com/a/status/1"}},
		Digest: "- Real. (@a https://x.com/a/status/1)\n- Invented. (@a https://x.com/a/status/99999)\n",
	}})

	if got.UnmatchedCitations != 1 {
		t.Errorf("unmatchedCitations = %d, want 1", got.UnmatchedCitations)
	}
	// the invented citation must not inflate anyone's hit rate
	if got.Cited != 1 || got.Kept != 1 {
		t.Errorf("cited/kept = %d/%d, want 1/1", got.Cited, got.Kept)
	}
	if a := got.ByHandle["@a"]; a.Cited != 1 {
		t.Errorf("@a cited = %d, want 1", a.Cited)
	}
}

func TestSummarizeCitationsEmptyInput(t *testing.T) {
	got := SummarizeCitations(nil)

	if got.Digests != 0 || got.Kept != 0 || got.Cited != 0 || got.CiteRate != 0 {
		t.Errorf("empty summary should be zero: %+v", got)
	}
	if got.ByHandle == nil {
		t.Error("ByHandle must be a usable empty map, not nil: callers index it directly")
	}
}

func citationKey(a Artifact) string {
	return artifactPrefix + strings.ReplaceAll(a.Timestamp, ":", "-") +
		"-twitter-digest-" + strings.ToLower(a.Language) + ".json"
}

func seedCitations(t *testing.T, store storage.Store, artifacts []Artifact) {
	t.Helper()
	for _, a := range artifacts {
		putArtifact(t, store, citationKey(a), a)
	}
}

func TestLoadCitations(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}
	seedCitations(t, store, citationFixture())

	got, err := LoadCitations(context.Background(), store, "")
	if err != nil {
		t.Fatalf("LoadCitations: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d artifacts, want 3", len(got))
	}
	if got[0].Timestamp != "2026-07-04T16:00:00Z" {
		t.Errorf("artifacts should come back oldest first, got %q", got[0].Timestamp)
	}
	if len(got[0].Kept) != 3 || got[0].Digest == "" {
		t.Errorf("kept posts or digest did not survive the round trip: %+v", got[0])
	}
}

// The four artifacts written before the language suffix existed are named
// "...-twitter-digest.json". They hold real digests and must not be skipped.
func TestLoadCitationsIncludesPreLanguageArtifacts(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}
	putArtifact(t, store, artifactPrefix+"2026-07-04T16-00-07Z-twitter-digest.json", Artifact{
		Timestamp: "2026-07-04T16:00:07Z",
		Kept:      []sources.Tweet{{ID: "1", Handle: "@a", URL: "https://x.com/a/status/1"}},
		Digest:    "- Old format. (@a https://x.com/a/status/1)\n",
	})

	got, err := LoadCitations(context.Background(), store, "")
	if err != nil {
		t.Fatalf("LoadCitations: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d artifacts, want 1: the pre-language naming was skipped", len(got))
	}
}

func TestLoadCitationsIgnoresForeignKeys(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}
	seedCitations(t, store, citationFixture())
	if err := store.Put(context.Background(), artifactPrefix+"2026-07-04T16-00-00Z-weekly-deepdive.json",
		[]byte(`{"ts":"2026-07-04T16:00:00Z","brief":"not a digest"}`)); err != nil {
		t.Fatal(err)
	}

	got, err := LoadCitations(context.Background(), store, "")
	if err != nil {
		t.Fatalf("a neighbouring project's artifact must not break the report: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d artifacts, want 3 (only twitter-digest keys)", len(got))
	}
}

func TestLoadCitationsSince(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}
	seedCitations(t, store, citationFixture())

	got, err := LoadCitations(context.Background(), store, "2026-07-05")
	if err != nil {
		t.Fatalf("LoadCitations: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d artifacts, want 1", len(got))
	}
	if got[0].Timestamp != "2026-07-05T16:00:00Z" {
		t.Errorf("kept the wrong artifact: %q", got[0].Timestamp)
	}
}

func TestLoadCitationsNoDataYet(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}

	got, err := LoadCitations(context.Background(), store, "")
	if err != nil {
		t.Fatalf("an empty archive is not an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d artifacts, want 0", len(got))
	}
}

// row returns one handle's data cells from the report table. Data rows have no spaces
// inside any cell, so whitespace splitting is exact (unlike the header).
func row(t *testing.T, out, handle string) []string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == handle {
			return fields
		}
	}
	t.Fatalf("no table row for %q in:\n%s", handle, out)
	return nil
}

// reportStore seeds both archives: the fetch records the fetch columns come from, and
// the digests the citation columns come from. fixtureRuns keeps posts 1, 2, 5 (@a) and
// 6 (@c) and keeps nothing at all for @b.
//
// The digest is dated 2026-07-06, before the fetch window, exactly as the real archives
// sit: digests since early July, fetch records only from the measurement week. Its
// numbers are chosen not to collide with any percentage the fetch half already prints
// (66.7, 16.7, 75.0, 100.0, 0.0), so a test asserting on 50.0% cannot pass by accident.
func reportStore(t *testing.T) storage.Store {
	t.Helper()
	store := &storage.FS{Root: t.TempDir()}
	ctx := context.Background()
	for _, run := range fixtureRuns() {
		data, err := json.Marshal(run)
		if err != nil {
			t.Fatal(err)
		}
		key := sourceStatsPrefix + strings.ReplaceAll(run.Timestamp, ":", "-") + "-twitter-digest.json"
		if err := store.Put(ctx, key, data); err != nil {
			t.Fatal(err)
		}
	}
	seedCitations(t, store, []Artifact{{
		Timestamp: "2026-07-06T16:00:00Z", Language: "English",
		Kept: []sources.Tweet{
			{ID: "1", Handle: "@a", URL: "https://x.com/a/status/1"},
			{ID: "2", Handle: "@a", URL: "https://x.com/a/status/2"},
			{ID: "5", Handle: "@a", URL: "https://x.com/a/status/5"},
			{ID: "6", Handle: "@c", URL: "https://x.com/c/status/6"},
		},
		Digest: "## World News\n\n- One. (@a https://x.com/a/status/1)\n- Six. (@c https://x.com/c/status/6)\n",
	}})
	return store
}

func TestSourcesReportCitationColumns(t *testing.T) {
	var buf bytes.Buffer
	if err := SourcesReport(context.Background(), reportStore(t), &buf, ""); err != nil {
		t.Fatalf("SourcesReport: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "CITED") || !strings.Contains(out, "CITE%") {
		t.Fatalf("citation columns missing from the table header:\n%s", out)
	}
	// CITED and CITE% sit immediately after KEEP%, as the quality counterpart to it.
	// @a fed 3 posts to the model and got 1 into print: a 75% keep rate next to a
	// 33.3% cite rate is the gap this whole column exists to expose.
	a := row(t, out, "@a")
	if a[5] != "1" || a[6] != "33.3%" {
		t.Errorf("@a cited/cite%% cells = %q/%q, want \"1\"/\"33.3%%\"; full row %v", a[5], a[6], a)
	}
	c := row(t, out, "@c")
	if c[5] != "1" || c[6] != "100.0%" {
		t.Errorf("@c cited/cite%% cells = %q/%q, want \"1\"/\"100.0%%\"; full row %v", c[5], c[6], c)
	}
}

// @b never survived the filter, so the model was never asked about it. A 0% there
// would read as "the editor rejected it", which is a different and much harsher claim.
func TestSourcesReportLeavesCitationsBlankWhenNeverSeen(t *testing.T) {
	var buf bytes.Buffer
	if err := SourcesReport(context.Background(), reportStore(t), &buf, ""); err != nil {
		t.Fatalf("SourcesReport: %v", err)
	}

	b := row(t, buf.String(), "@b")
	if b[5] != "-" || b[6] != "-" {
		t.Errorf("@b cited/cite%% cells = %q/%q, want \"-\"/\"-\"; full row %v", b[5], b[6], b)
	}
}

// The digest archive is months older than the fetch archive, so the two windows are
// not the same period and the header must never imply they are.
func TestSourcesReportStatesTheCitationWindow(t *testing.T) {
	var buf bytes.Buffer
	if err := SourcesReport(context.Background(), reportStore(t), &buf, ""); err != nil {
		t.Fatalf("SourcesReport: %v", err)
	}
	out := buf.String()

	// the digest archive starts before the fetch archive, so its own dates must appear
	if !strings.Contains(out, "2026-07-06") {
		t.Errorf("citation window not stated in the header:\n%s", out)
	}
	// 2 of the 4 posts that reached the model were cited
	if !strings.Contains(out, "50.0%") {
		t.Errorf("archive-wide citation rate not stated in the header:\n%s", out)
	}
}

// The two archives are independent, so the citation half has to stand on its own. This
// is the state the repo is actually in tonight: months of digests, zero fetch records.
// Bailing out early here would throw away the only measurement available.
func TestSourcesReportWithDigestsButNoFetchRecords(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}
	seedCitations(t, store, citationFixture())

	var buf bytes.Buffer
	if err := SourcesReport(context.Background(), store, &buf, ""); err != nil {
		t.Fatalf("no fetch records is not an error: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, sourceStatsPrefix) {
		t.Errorf("report should say the fetch archive is empty:\n%s", out)
	}
	// the fixture is 3 of 5 cited across 3 digests
	if !strings.Contains(out, "60.0%") {
		t.Errorf("citation rate is available and must still be reported:\n%s", out)
	}
	if !strings.Contains(out, "2026-07-04") {
		t.Errorf("citation window missing:\n%s", out)
	}
}
