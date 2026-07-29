package twitterdigest

import (
	"context"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/sl6117/automations/internal/storage"
)

// LangVerdicts is one language's judge funnel across stored digeests. The floor (ShippedFails/Judged) is available over the whole archive;
// the funnel columns (Measurable onward) only over artifacts written after the instrumentation bite.
type LangVerdicts struct {
	Language     string
	Judged       int // artifacts with a shipped verdict
	ShippedFails int // shipped verdict failed faithfulness: the floor, undercounts firing
	Measurable   int // carry an initial verdict, so the funnel can be computed
	InitialFails int // faithfulness failed on the FIRST draft: how often the loop fired
	Adopted      int // a revision was adopted
}

// VerdictsSummary is the archive-wide judge funnel, per language.
type VerdictsSummary struct {
	Digests        int // artifacts with a shipped verdict
	Unmeasurable   int // judged but pre-instrumentation: floor only, no funnel
	JudgeErrors    int // recorded a judge error and no verdict: excluded from both
	FirstTimestamp string
	LastTimestamp  string
	ByLanguage     []LangVerdicts
}

// SummarizeVerdicts buckets stored digest artifacts into a per-lanugage judge funnel.
// An artifact with no shipped verdict is not funnel data: a judge error is tallied separately, anything else is simply skipped.
func SummarizeVerdicts(artifacts []Artifact) VerdictsSummary {
	var s VerdictsSummary
	byLang := map[string]*LangVerdicts{}
	var order []string
	for _, a := range artifacts {
		if a.Judge == nil {
			if a.JudgeError != "" {
				s.JudgeErrors++
			}
			continue
		}
		s.Digests++
		if s.FirstTimestamp == "" || a.Timestamp < s.FirstTimestamp {
			s.FirstTimestamp = a.Timestamp
		}
		if a.Timestamp > s.LastTimestamp {
			s.LastTimestamp = a.Timestamp
		}
		l := byLang[a.Language]
		if l == nil {
			l = &LangVerdicts{Language: a.Language}
			byLang[a.Language] = l
			order = append(order, a.Language)
		}
		l.Judged++
		if !a.Judge.Faithfulness.Pass {
			l.ShippedFails++
		}
		if a.InitialJudge != nil {
			l.Measurable++
			if !a.InitialJudge.Faithfulness.Pass {
				l.InitialFails++
			}
		} else {
			s.Unmeasurable++
		}
		if a.RevisionAdopted {
			l.Adopted++
		}
	}
	for _, lang := range order {
		s.ByLanguage = append(s.ByLanguage, *byLang[lang])
	}
	sort.Slice(s.ByLanguage, func(i, j int) bool {
		if s.ByLanguage[i].Judged != s.ByLanguage[j].Judged {
			return s.ByLanguage[i].Judged > s.ByLanguage[j].Judged
		}
		return s.ByLanguage[i].Language < s.ByLanguage[j].Language
	})
	return s
}

// VerdictsReport writes the per-language judge funnel. LoadCitations is the shared artifact loader.
// Nothing about it is citation-specific
func VerdictsReport(ctx context.Context, store storage.Store, w io.Writer, since string) error {
	artifacts, err := LoadCitations(ctx, store, since)
	if err != nil {
		return err
	}
	v := SummarizeVerdicts(artifacts)
	if v.Digests == 0 {
		fmt.Fprintf(w, "No judged digests yet under %s\n", artifactPrefix)
		if v.JudgeErrors > 0 {
			fmt.Fprintf(w, "(%d run(s) recorded a judge error)\n", v.JudgeErrors)
		}
		return nil
	}
	fmt.Fprintf(w, "judge funnel - %d digests, %s to %s\n", v.Digests, dayOf(v.FirstTimestamp), dayOf(v.LastTimestamp))
	if v.Unmeasurable > 0 {
		fmt.Fprintf(w, "%d digest(s) predate funnel instrumentation - FIRE%%/ADOPT%% cover only the %d measured\n",
			v.Unmeasurable, v.Digests-v.Unmeasurable)
	}
	if v.JudgeErrors > 0 {
		fmt.Fprintf(w, "%d run(s) recorded a judge error and are excluded\n", v.JudgeErrors)
	}
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "LANGUAGE\tJUDGED\tFAITH-FAIL%\tMEASURED\tFIRE%\tADOPT%")
	for _, l := range v.ByLanguage {
		fire, adopt := "-", "-"
		if l.Measurable > 0 {
			fire = fmt.Sprintf("%.1f%%", pct(l.InitialFails, l.Measurable))
		}
		if l.InitialFails > 0 {
			adopt = fmt.Sprintf("%.1f%%", pct(l.Adopted, l.InitialFails))
		}
		fmt.Fprintf(tw, "%s\t%d\t%.1f%%\t%d\t%s\t%s\n",
			l.Language, l.Judged, pct(l.ShippedFails, l.Judged), l.Measurable, fire, adopt)
	}
	return tw.Flush()
}
