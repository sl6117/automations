package twitterdigest

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sl6117/automations/internal/storage"
)

// BacktestParams is the ranked-filter config replayed over stored runs.
type BacktestParams struct {
	Lookback      time.Duration
	MinEngagement int
	RelativeK     float64
	MaxPerAuthor  int
	DigestBudget  int
	Alpha         float64
}

// DefaultBacktestParams matches config.json caps and the agreed a / 7-day window.
func DefaultBacktestParams() BacktestParams {
	return BacktestParams{
		Lookback:      7 * 24 * time.Hour,
		MinEngagement: 100,
		RelativeK:     0.5,
		MaxPerAuthor:  3,
		DigestBudget:  32,
		Alpha:         0.7,
	}
}

// RankBacktestReport loads sourcestats + digests and prints the ranked-filter comparison.
func RankBacktestReport(ctx context.Context, store storage.Store, w io.Writer, since string, p BacktestParams) error {
	runs, err := LoadSourceStats(ctx, store, since)
	if err != nil {
		return err
	}
	artifacts, err := LoadCitations(ctx, store, since)
	if err != nil {
		return err
	}
	writeFilterBacktest(w, backtestRankedFilter(runs, artifacts, p), p)
	return nil
}

func writeFilterBacktest(w io.Writer, b FilterBacktest, p BacktestParams) {
	fmt.Fprintf(w, "rank-budget backtest — %d xapi runs\n", b.Runs)
	fmt.Fprintf(w, "params: lookback=%s minEngagement=%d relativeK=%.2f maxPerAuthor=%d digestBudget=%d alpha=%.2f\n",
		p.Lookback, p.MinEngagement, p.RelativeK, p.MaxPerAuthor, p.DigestBudget, p.Alpha)
	fmt.Fprintf(w, "kept: baseline %d → ranked %d\n", b.BaselineKept, b.RankedKept)
	fmt.Fprintf(w, "cite recall: baseline %.1f%% → ranked %.1f%% (%d/%d → %d/%d cited ids kept)\n",
		b.BaselineCiteRecall*100, b.RankedCiteRecall*100,
		b.BaselineCitedKept, b.Cited, b.RankedCitedKept, b.Cited)
	fmt.Fprintf(w, "RT kept: baseline %d → ranked %d\n", b.BaselineRTKept, b.RankedRTKept)
	fmt.Fprintf(w, "newswire recovered: %d (was low_engagement, kept under ranked)\n", b.NewswireRecovered)
}

// FilterBacktest compares production keep flags to applyRankedFilter.
// Cite recall = fraction of cited status ids (that appear in fetched posts)
// that each filter kept - the editorial value signal.
type FilterBacktest struct {
	Runs               int
	BaselineKept       int
	RankedKept         int
	Cited              int
	BaselineCitedKept  int
	RankedCitedKept    int
	BaselineCiteRecall float64
	RankedCiteRecall   float64
	BaselineRTKept     int
	RankedRTKept       int
	NewswireRecovered  int // DropLowEngagement under baseline, kept under ranked
}

func backtestRankedFilter(runs []SourceStats, artifacts []Artifact, p BacktestParams) FilterBacktest {
	cited := map[string]bool{}

	for _, a := range artifacts {
		if strings.TrimSpace(a.Digest) == "" {
			continue
		}
		for _, id := range parseCitations(a.Digest) {
			cited[id] = true
		}
	}

	fetchedCited := map[string]bool{}
	baselineCited := map[string]bool{}
	rankedCited := map[string]bool{}
	var out FilterBacktest

	for _, run := range runs {
		if run.Source != "xapi" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, run.Timestamp)
		if err != nil {
			continue
		}
		out.Runs++
		medians := handleMedians(runs, p.Lookback, ts)
		ranked := applyAddOnly(run.Posts, medians, RankParams{
			MinEngagement: p.MinEngagement,
			RelativeK:     p.RelativeK,
			MaxPerAuthor:  p.MaxPerAuthor,
			DigestBudget:  p.DigestBudget,
			Alpha:         p.Alpha,
			RunAt:         ts,
		})
		for i, base := range run.Posts {
			if base.Kept {
				out.BaselineKept++
				if base.IsRetweet {
					out.BaselineRTKept++
				}
			}
			r := ranked[i]
			if r.Kept {
				out.RankedKept++
				if r.IsRetweet {
					out.RankedRTKept++
				}
			}
			if base.DropReason == DropLowEngagement && r.Kept {
				out.NewswireRecovered++
			}
			if !cited[base.ID] {
				continue
			}
			fetchedCited[base.ID] = true
			if base.Kept {
				baselineCited[base.ID] = true
			}
			if r.Kept {
				rankedCited[base.ID] = true
			}
		}
	}
	out.Cited = len(fetchedCited)
	out.BaselineCitedKept = len(baselineCited)
	out.RankedCitedKept = len(rankedCited)
	if out.Cited > 0 {
		out.BaselineCiteRecall = float64(out.BaselineCitedKept) / float64(out.Cited)
		out.RankedCiteRecall = float64(out.RankedCitedKept) / float64(out.Cited)
	}
	return out

}
