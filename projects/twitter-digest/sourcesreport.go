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
const readCostUSD = 0.005

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
	Runs           int
	SkippedRuns    int // non-xapi runs, excluded from every figure below
	TruncatedRuns  int
	Fetched        int
	Kept           int
	Reads          int
	CostUSD        float64
	FirstTimestamp string
	LastTimestamp  string
	Handles        []HandleReport
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
	s := Summarize(runs)
	if s.Runs == 0 {
		fmt.Fprintf(w, "No X fetch records yet under %s\n", sourceStatsPrefix)
		if s.SkippedRuns > 0 {
			fmt.Fprintf(w, "(%d non-xapi run(s) skipped)\n", s.SkippedRuns)
		}
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
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "HANDLE\tFETCHED\tSHARE\tKEPT\tKEEP%\tMED ENG\tMAX ENG\t$/KEPT\tLOW\tDUP\tCAP")
	for _, h := range s.Handles {
		perKept := "-"
		if h.Kept > 0 {
			perKept = fmt.Sprintf("$%.3f", h.CostPerKeptUSD)
		}
		fmt.Fprintf(tw, "%s\t%d\t%.1f%%\t%d\t%.1f%%\t%d\t%d\t%s\t%d\t%d\t%d\n",
			h.Handle, h.Fetched, h.VolumeShare*100, h.Kept, h.KeepRate*100,
			h.MedianEngagement, h.MaxEngagement, perKept,
			h.DroppedLowEngagement, h.DroppedDuplicate, h.DroppedPerAuthorCap)
	}
	return tw.Flush()
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
