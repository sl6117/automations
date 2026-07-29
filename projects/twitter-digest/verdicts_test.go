package twitterdigest

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/storage"
)

func vPass() *JudgeReport {
	return &JudgeReport{
		Faithfulness: Verdict{Pass: true}, TopicRouting: Verdict{Pass: true},
		Coverage: Verdict{Pass: true}, Clarity: Verdict{Pass: true},
	}
}

func vFail() *JudgeReport {
	return &JudgeReport{
		Faithfulness: Verdict{Pass: false, Reason: "misattributed quote"}, TopicRouting: Verdict{Pass: true},
		Coverage: Verdict{Pass: true}, Clarity: Verdict{Pass: true},
	}
}

// verdictFixture is a deliberately lopsided archive so the language gap is unmissable and
// every count is hand-checkable. English exercises the whole funnel; Korean fires on every
// run and never once adopts; one old English artifact predates the instrumentation (floor
// only); one recorded a judge error and carries no verdict at all.
//
//	English judged: 4 (1 adopted-clean, 1 fired-unadopted, 1 clean, 1 pre-instrumentation)
//	English shipped faithfulness fails: 1 (the fired-unadopted one)
//	Korean judged: 2, both shipped failing, both fired, none adopted
func verdictFixture() []Artifact {
	return []Artifact{
		// fired, revision adopted -> ships a pass
		{Timestamp: "2026-07-28T16:00:00Z", Language: "English", Digest: "d", Judge: vPass(), InitialJudge: vFail(), RevisionAdopted: true},
		// fired, revision failed -> ships the original failing draft
		{Timestamp: "2026-07-28T16:00:01Z", Language: "English", Digest: "d", Judge: vFail(), InitialJudge: vFail()},
		// clean first try
		{Timestamp: "2026-07-28T16:00:02Z", Language: "English", Digest: "d", Judge: vPass(), InitialJudge: vPass()},
		// Korean: both fired and neither adopted, so both ship failing
		{Timestamp: "2026-07-28T16:00:03Z", Language: "Korean", Digest: "d", Judge: vFail(), InitialJudge: vFail()},
		{Timestamp: "2026-07-28T16:00:04Z", Language: "Korean", Digest: "d", Judge: vFail(), InitialJudge: vFail()},
		// pre-instrumentation: has a shipped verdict, no InitialJudge. Counts for the
		// floor, cannot count for the funnel.
		{Timestamp: "2026-07-20T16:00:00Z", Language: "English", Digest: "d", Judge: vPass()},
		// judge errored: no verdict, so it is neither floor nor funnel data
		{Timestamp: "2026-07-19T16:00:00Z", Language: "English", Digest: "d", JudgeError: "judge timed out"},
	}
}

func langVerdicts(t *testing.T, s VerdictsSummary, lang string) LangVerdicts {
	t.Helper()
	for _, l := range s.ByLanguage {
		if l.Language == lang {
			return l
		}
	}
	t.Fatalf("no row for %q in %+v", lang, s.ByLanguage)
	return LangVerdicts{}
}

func TestSummarizeVerdictsTotals(t *testing.T) {
	got := SummarizeVerdicts(verdictFixture())

	if got.Digests != 6 {
		t.Errorf("digests = %d, want 6 (judged artifacts; the judge-error one is excluded)", got.Digests)
	}
	if got.JudgeErrors != 1 {
		t.Errorf("judgeErrors = %d, want 1", got.JudgeErrors)
	}
	if got.Unmeasurable != 1 {
		t.Errorf("unmeasurable = %d, want 1 (the pre-instrumentation artifact)", got.Unmeasurable)
	}
	if got.FirstTimestamp != "2026-07-20T16:00:00Z" || got.LastTimestamp != "2026-07-28T16:00:04Z" {
		t.Errorf("window = %s..%s, want the judged span (the errored run is not judged)", got.FirstTimestamp, got.LastTimestamp)
	}
}

func TestSummarizeVerdictsEnglishFunnel(t *testing.T) {
	e := langVerdicts(t, SummarizeVerdicts(verdictFixture()), "English")

	if e.Judged != 4 || e.ShippedFails != 1 {
		t.Errorf("English judged/shippedFails = %d/%d, want 4/1", e.Judged, e.ShippedFails)
	}
	// only the 3 instrumented artifacts are measurable for the funnel
	if e.Measurable != 3 {
		t.Errorf("English measurable = %d, want 3", e.Measurable)
	}
	if e.InitialFails != 2 {
		t.Errorf("English initialFails = %d, want 2 (fired on the adopted and the unadopted)", e.InitialFails)
	}
	if e.Adopted != 1 {
		t.Errorf("English adopted = %d, want 1", e.Adopted)
	}
}

// The whole point of the report: Korean's shipped failure rate is the floor gap, available
// over the entire archive; the funnel shows WHY - it fires every time and never adopts.
func TestSummarizeVerdictsKoreanShowsTheGap(t *testing.T) {
	k := langVerdicts(t, SummarizeVerdicts(verdictFixture()), "Korean")

	if k.Judged != 2 || k.ShippedFails != 2 {
		t.Errorf("Korean judged/shippedFails = %d/%d, want 2/2 (100%% floor)", k.Judged, k.ShippedFails)
	}
	if k.Measurable != 2 || k.InitialFails != 2 {
		t.Errorf("Korean measurable/initialFails = %d/%d, want 2/2", k.Measurable, k.InitialFails)
	}
	if k.Adopted != 0 {
		t.Errorf("Korean adopted = %d, want 0: the revision never rescues Korean", k.Adopted)
	}
}

func TestSummarizeVerdictsRanksByVolume(t *testing.T) {
	got := SummarizeVerdicts(verdictFixture())

	if len(got.ByLanguage) != 2 {
		t.Fatalf("got %d languages, want 2: %+v", len(got.ByLanguage), got.ByLanguage)
	}
	// English has 4 judged, Korean 2, so English sorts first
	if got.ByLanguage[0].Language != "English" || got.ByLanguage[1].Language != "Korean" {
		t.Errorf("order = %s,%s, want English,Korean (by judged desc)", got.ByLanguage[0].Language, got.ByLanguage[1].Language)
	}
}

func TestSummarizeVerdictsEmpty(t *testing.T) {
	got := SummarizeVerdicts(nil)

	if got.Digests != 0 || len(got.ByLanguage) != 0 {
		t.Errorf("empty summary should be zero: %+v", got)
	}
}

func seedVerdicts(t *testing.T, store storage.Store, artifacts []Artifact) {
	t.Helper()
	for _, a := range artifacts {
		putArtifact(t, store, citationKey(a), a)
	}
}

func TestVerdictsReportShowsPerLanguageFloorAndFunnel(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}
	seedVerdicts(t, store, verdictFixture())

	var buf bytes.Buffer
	if err := VerdictsReport(context.Background(), store, &buf, ""); err != nil {
		t.Fatalf("VerdictsReport: %v", err)
	}
	out := buf.String()

	e := row(t, out, "English")
	// LANGUAGE JUDGED FAITH-FAIL% MEASURED FIRE% ADOPT%
	if e[1] != "4" || e[2] != "25.0%" {
		t.Errorf("English judged/floor cells = %q/%q, want 4/25.0%%; row %v", e[1], e[2], e)
	}
	if e[4] != "66.7%" || e[5] != "50.0%" {
		t.Errorf("English fire/adopt cells = %q/%q, want 66.7%%/50.0%%; row %v", e[4], e[5], e)
	}
	k := row(t, out, "Korean")
	if k[2] != "100.0%" || k[5] != "0.0%" {
		t.Errorf("Korean floor/adopt cells = %q/%q, want 100.0%%/0.0%%; row %v", k[2], k[5], k)
	}
}

// Tonight's real archive is 61 pre-instrumentation digests: the floor must render even
// when the funnel columns have nothing to show yet.
func TestVerdictsReportFloorOnlyWhenNoInstrumentation(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}
	seedVerdicts(t, store, []Artifact{
		{Timestamp: "2026-07-20T16:00:00Z", Language: "English", Digest: "d", Judge: vPass()},
		{Timestamp: "2026-07-21T16:00:00Z", Language: "Korean", Digest: "d", Judge: vFail()},
	})

	var buf bytes.Buffer
	if err := VerdictsReport(context.Background(), store, &buf, ""); err != nil {
		t.Fatalf("VerdictsReport: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "English") || !strings.Contains(out, "Korean") {
		t.Fatalf("both languages should appear:\n%s", out)
	}
	// FIRE%/ADOPT% have no measured runs to divide, so they must read as "-" not 0.0% or NaN
	k := row(t, out, "Korean")
	if k[2] != "100.0%" {
		t.Errorf("Korean floor = %q, want 100.0%%; row %v", k[2], k)
	}
	if k[4] != "-" || k[5] != "-" {
		t.Errorf("Korean fire/adopt = %q/%q, want -/- (nothing measured yet); row %v", k[4], k[5], k)
	}
}

func TestVerdictsReportNoData(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}

	var buf bytes.Buffer
	if err := VerdictsReport(context.Background(), store, &buf, ""); err != nil {
		t.Fatalf("no data is not an error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("report should say something when there are no judged digests yet")
	}
}
