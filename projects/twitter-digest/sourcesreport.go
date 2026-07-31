package twitterdigest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/sl6117/automations/internal/storage"
)

// X pay-per-use rate. Reads are billed per page of 50 regardless of how many posts on that page are useful,
// which is why cost is apportioned by volume share below rather than charged to the posts that survived.
const (
	readCostUSD = 0.005
	// thinOwnTextLimit is the character cutoff for "thin" kept posts in the own-text summary.
	// Below this (and above zero) a non-retweet is substance-poor for a text digest.
	thinOwnTextLimit = 10
)

// HandleREport is one account's contribution to the fetch budget across stored runs.
type HandleReport struct {
	Handle               string
	Fetched              int
	Kept                 int
	VolumeShare          float64
	KeepRate             float64
	CostUSD              float64
	CostPerKeptUSD       float64 // 0 when nothing was kept: undefined, not infinite
	MedianEngagement     int
	MaxEngagement        int
	DroppedLowEngagement int
	DroppedDuplicate     int
	DroppedPerAuthorCap  int
}

// SourcesSummary is the run-level view over the stored fetch records.
type SourcesSummary struct {
	Runs                  int
	SkippedRuns           int // non-xapi runs, excluded from every figure below
	TruncatedRuns         int
	Fetched               int
	Kept                  int
	Reads                 int
	CostUSD               float64
	FirstTimestamp        string
	LastTimestamp         string
	OwnTextMeasuredRuns   int // runs that carry own-text / quote instrumentation
	OwnTextUnmeasuredRuns int // xapi runs that predate those fields
	KeptMeasured          int // kept posts on measured runs only
	KeptEmptyOwnText      int // kept, non-retweet, OwnTextLength == 0
	KeptThinOwnText       int // kept, non-retweet, 0 < OwnTextLength < thinOwnTextLimit
	Handles               []HandleReport
}

// LoadSourcesStats reads every stored fetch record, oldest first (key sort lexically by timestamp).
// Since is an optional YYYY-MM-DD lower bound. An empty archive is not an error:
// before the first run there is simply nothing to report.
func LoadSourceStats(ctx context.Context, store storage.Store, since string) ([]SourceStats, error) {
	keys, err := store.List(ctx, sourceStatsPrefix)
	if err != nil {
		return nil, fmt.Errorf("list source stats: %w", err)
	}
	runs := make([]SourceStats, 0, len(keys))
	for _, key := range keys {
		if since != "" && strings.TrimPrefix(key, sourceStatsPrefix) < since {
			continue
		}
		data, err := store.Get(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("get %q: %w", key, err)
		}
		var run SourceStats
		if err := json.Unmarshal(data, &run); err != nil {
			return nil, fmt.Errorf("parse %q: %w", key, err)
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// ownTextMeasured reports whether a run's rows carry the own-text / quote fields.
// Pre-instrumentation blobs deserialize with OwnTextLength=0 and IsQuote=false for every post
// treating those zeros as empty would make the whole archive look thin.
// own-text length or a quote flag is enough to trust the run. IsRetweet alone is not:
// that flag existed before own-text instrumentation.
func ownTextMeasured(run SourceStats) bool {
	for _, p := range run.Posts {
		if p.OwnTextLength > 0 || p.IsQuote {
			return true
		}
	}
	return false
}

// countOwnText classifies one kept post for the own-text summary. Retweets are skipped:
// their own-text is empty by design and is not a substance signal.
func countOwnText(s *SourcesSummary, p PostObservation) {
	if !p.Kept {
		return
	}
	s.KeptMeasured++
	if p.IsRetweet {
		return
	}
	switch {
	case p.OwnTextLength == 0:
		s.KeptEmptyOwnText++
	case p.OwnTextLength < thinOwnTextLimit:
		s.KeptThinOwnText++
	}
}

// Summarize aggregates stored runs into a per-handle ranking. Only xapi runs count:
// mock runs from local development would otherwise skew every ratio
func Summarize(runs []SourceStats) SourcesSummary {
	var s SourcesSummary
	type acc struct {
		report      HandleReport
		engagements []int
	}
	byHandle := map[string]*acc{}
	var order []string
	for _, run := range runs {
		if run.Source != "xapi" {
			s.SkippedRuns++
			continue
		}
		s.Runs++
		s.Reads += run.Reads
		if run.Truncated {
			s.TruncatedRuns++
		}
		measured := ownTextMeasured(run)
		if measured {
			s.OwnTextMeasuredRuns++
		} else {
			s.OwnTextUnmeasuredRuns++
		}
		if s.FirstTimestamp == "" || run.Timestamp < s.FirstTimestamp {
			s.FirstTimestamp = run.Timestamp
		}
		if run.Timestamp > s.LastTimestamp {
			s.LastTimestamp = run.Timestamp
		}
		for _, p := range run.Posts {
			a := byHandle[p.Handle]
			if a == nil {
				a = &acc{report: HandleReport{Handle: p.Handle}}
				byHandle[p.Handle] = a
				order = append(order, p.Handle)
			}
			s.Fetched++
			a.report.Fetched++
			engagement := p.Likes + p.Reposts
			a.engagements = append(a.engagements, engagement)
			if engagement > a.report.MaxEngagement {
				a.report.MaxEngagement = engagement
			}
			if p.Kept {
				s.Kept++
				a.report.Kept++
			}
			if measured {
				countOwnText(&s, p)
			}
			switch p.DropReason {
			case DropLowEngagement:
				a.report.DroppedLowEngagement++
			case DropDuplicate:
				a.report.DroppedDuplicate++
			case DropPerAuthorCap:
				a.report.DroppedPerAuthorCap++
			}
		}
	}
	s.CostUSD = float64(s.Reads) * readCostUSD
	for _, handle := range order {
		a := byHandle[handle]
		r := a.report
		r.MedianEngagement = median(a.engagements)
		if s.Fetched > 0 {
			r.VolumeShare = float64(r.Fetched) / float64(s.Fetched)
		}
		if r.Fetched > 0 {
			r.KeepRate = float64(r.Kept) / float64(r.Fetched)
		}
		r.CostUSD = s.CostUSD * r.VolumeShare
		if r.Kept > 0 {
			r.CostPerKeptUSD = r.CostUSD / float64(r.Kept)
		}
		s.Handles = append(s.Handles, r)
	}
	sort.Slice(s.Handles, func(i, j int) bool {
		if s.Handles[i].Fetched != s.Handles[j].Fetched {
			return s.Handles[i].Fetched > s.Handles[j].Fetched
		}
		return s.Handles[i].Handle < s.Handles[j].Handle
	})
	return s
}

// SourcesReport writes the human-readable ranking used to curate the list.
func SourcesReport(ctx context.Context, store storage.Store, w io.Writer, since string) error {
	runs, err := LoadSourceStats(ctx, store, since)
	if err != nil {
		return err
	}
	artifacts, err := LoadCitations(ctx, store, since)
	if err != nil {
		return err
	}
	s := Summarize(runs)
	c := SummarizeCitations(artifacts)
	if s.Runs == 0 {
		fmt.Fprintf(w, "No X fetch records yet under %s\n", sourceStatsPrefix)
		if s.SkippedRuns > 0 {
			fmt.Fprintf(w, "(%d non-xapi run(s) skipped)\n", s.SkippedRuns)
		}
		writeCitationSummary(w, c)
		fmt.Fprintln(w)
		writeCitationTable(w, c)
		return nil
	}
	fmt.Fprintf(w, "X source report - %d runs, %s to %s\n", s.Runs, dayOf(s.FirstTimestamp), dayOf(s.LastTimestamp))
	fmt.Fprintf(w, "%d posts fetched, %d kept (%.1f%%), %d reads, $%.2f\n",
		s.Fetched, s.Kept, pct(s.Kept, s.Fetched), s.Reads, s.CostUSD)
	fmt.Fprintf(w, "%d of %d runs hit the page cap - the oldest posts of those days were never fetched\n",
		s.TruncatedRuns, s.Runs)
	if s.SkippedRuns > 0 {
		fmt.Fprintf(w, "%d non-xapi run(s) skipped\n", s.SkippedRuns)
	}
	writeOwnTextSummary(w, s)
	writeCitationSummary(w, c)
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "HANDLE\tFETCHED\tSHARE\tKEPT\tKEEP%\tCITED\tCITE%\tMED ENG\tMAX ENG\t$/KEPT\tLOW\tDUP\tCAP")
	for _, h := range s.Handles {
		perKept := "-"
		if h.Kept > 0 {
			perKept = fmt.Sprintf("$%.3f", h.CostPerKeptUSD)
		}
		cited, citeRate := "-", "-"
		if r, ok := c.ByHandle[h.Handle]; ok {
			cited = fmt.Sprintf("%d", r.Cited)
			citeRate = fmt.Sprintf("%.1f%%", r.CiteRate*100)
		}
		fmt.Fprintf(tw, "%s\t%d\t%.1f%%\t%d\t%.1f%%\t%s\t%s\t%d\t%d\t%s\t%d\t%d\t%d\n",
			h.Handle, h.Fetched, h.VolumeShare*100, h.Kept, h.KeepRate*100,
			cited, citeRate,
			h.MedianEngagement, h.MaxEngagement, perKept,
			h.DroppedLowEngagement, h.DroppedDuplicate, h.DroppedPerAuthorCap)

	}
	return tw.Flush()
}

// writeCitationSummary states the digest archive's own window and rate. It prints even when no fetch records exist:
// the two archives are independent and the digest one is older.
func writeCitationSummary(w io.Writer, c CitationsSummary) {
	if c.Digests > 0 {
		fmt.Fprintf(w, "citations: %d digests, %s to %s - the digest cited %d of the %d posts that reached the model (%.1f%%)\n",
			c.Digests, dayOf(c.FirstTimestamp), dayOf(c.LastTimestamp), c.Cited, c.Kept, c.CiteRate*100)
	}
	if c.UnmatchedCitations > 0 {
		fmt.Fprintf(w, "%d citation(s) point at posts that were never fed to the model\n", c.UnmatchedCitations)
	}
}

// rankCitations orders the per-handle map into a stable slice: most kept posts first,
// alphabetial tie-break so the table is stable run to run.
func rankCitations(c CitationsSummary) []CitationReport {
	rows := make([]CitationReport, 0, len(c.ByHandle))
	for _, r := range c.ByHandle {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Kept != rows[j].Kept {
			return rows[i].Kept > rows[j].Kept
		}
		return rows[i].Handle < rows[j].Handle
	})
	return rows
}

// writeOwnTextSummary reports how many kept posts had nothing (or almost nothing) for a text digest to summarize.
// Silent when nothing has been measured yet so the line doesn't pretend Jul 29's zero-valued rows were empty.
func writeOwnTextSummary(w io.Writer, s SourcesSummary) {
	if s.OwnTextMeasuredRuns == 0 {
		return
	}
	fmt.Fprintf(w, "own-text: of %d kept posts across %d instrumented run(s), %d empty and %d thin (<%d chars)",
		s.KeptMeasured, s.OwnTextMeasuredRuns, s.KeptEmptyOwnText, s.KeptThinOwnText, thinOwnTextLimit)
	if s.OwnTextUnmeasuredRuns > 0 {
		fmt.Fprintf(w, "; %d run(s) predate own-text instrumentation", s.OwnTextUnmeasuredRuns)
	}
	fmt.Fprintln(w)
}

// writeCitationTable renders the per-handle hit rate. Only needed when there is no fetch table
// to carry CITED/CITE% inline; on empty input it prints nothing.
func writeCitationTable(w io.Writer, c CitationsSummary) {
	rows := rankCitations(c)
	if len(rows) == 0 {
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "HANDLE\tKEPT\tCITED\tCITE%")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%.1f%%\n", r.Handle, r.Kept, r.Cited, r.CiteRate*100)
	}
	tw.Flush()
}

// median returns the middle value, or the mean of the middle pair when the count is
// even. It sorts a copy so callers keep their slice order.
func median(values []int) int {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]int, len(values))
	copy(sorted, values)
	sort.Ints(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}
func pct(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole) * 100
}
func dayOf(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}
