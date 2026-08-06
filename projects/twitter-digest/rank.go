package twitterdigest

import (
	"sort"
	"time"
)

// RankParams controls the offline / future ranked filter. Alpha blends relative-to-median (1) vs engagement-per-hour (0).
type RankParams struct {
	MaxPerAuthor int
	DigestBudget int
	Alpha        float64
	RunAt        time.Time
}

// rankScore ranks one post. Cold-start (median < 1) falls back to eng/followers.
// Otherwise: alpha * (eng/median) + (1-alpha) * ((eng/ageHours)/median).
func rankScore(p PostObservation, handleMedian int, runAt time.Time, alpha float64) float64 {
	if handleMedian < 1 {
		return p.score()
	}
	eng := float64(p.engagement())
	ageH := 24.0 // unknown CreatedAt: don't let eph dominate
	if !p.CreatedAt.IsZero() {
		ageH = runAt.Sub(p.CreatedAt).Hours()
		if ageH < 1 {
			ageH = 1
		}
	}
	med := float64(handleMedian)
	relative := eng / med
	ephNorm := (eng / ageH) / med
	return alpha*relative + (1-alpha)*ephNorm
}

// handleMedians builds per-handle medians of non-RT engagement from xapi runs in
// (before-lookback, before). RTs and mock runs are excluded.
func handleMedians(prior []SourceStats, lookback time.Duration, before time.Time) map[string]int {
	windowStart := before.Add(-lookback)
	byHandle := make(map[string][]int)
	for _, run := range prior {
		if run.Source != "xapi" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, run.Timestamp)
		if err != nil || ts.Before(windowStart) || !ts.Before(before) {
			continue
		}
		for _, p := range run.Posts {
			if p.IsRetweet {
				continue
			}
			byHandle[p.Handle] = append(byHandle[p.Handle], p.engagement())
		}
	}
	out := make(map[string]int, len(byHandle))
	for h, vals := range byHandle {
		out[h] = median(vals)
	}
	return out
}

// applyRankedFilter is rank-only: no minEngagement floor. Same empty-own-text per author,
// and digest-budget caps as filter; ranking uses rankScore.
// Dedup is omitted here - PostObservation has no text (live filter keeps it).
func applyRankedFilter(obs []PostObservation, medians map[string]int, p RankParams) []PostObservation {
	out := make([]PostObservation, len(obs))
	copy(out, obs)
	eligible := make(map[string][]int)
	for i := range out {
		row := &out[i]
		row.Kept = false
		row.DropReason = ""
		switch {
		case !row.IsRetweet && row.OwnTextLength == 0:
			row.DropReason = DropEmptyOwnText
		default:
			row.Kept = true
			eligible[row.Handle] = append(eligible[row.Handle], i)
		}
	}
	rank := func(i int) float64 {
		return rankScore(out[i], medians[out[i].Handle], p.RunAt, p.Alpha)
	}
	if p.MaxPerAuthor > 0 {
		for _, idx := range eligible {
			if len(idx) <= p.MaxPerAuthor {
				continue
			}
			sort.SliceStable(idx, func(a, b int) bool {
				x, y := out[idx[a]], out[idx[b]]
				if x.IsRetweet != y.IsRetweet {
					return !x.IsRetweet
				}
				return rank(idx[a]) > rank(idx[b])
			})
			for _, i := range idx[p.MaxPerAuthor:] {
				out[i].Kept = false
				out[i].DropReason = DropPerAuthorCap
			}
		}
	}
	if p.DigestBudget > 0 {
		var keptIdx []int
		for i, row := range out {
			if row.Kept {
				keptIdx = append(keptIdx, i)
			}
		}
		if len(keptIdx) > p.DigestBudget {
			sort.SliceStable(keptIdx, func(a, b int) bool {
				return rank(keptIdx[a]) > rank(keptIdx[b])
			})
			for _, i := range keptIdx[p.DigestBudget:] {
				out[i].Kept = false
				out[i].DropReason = DropDigestBudget
			}
		}
	}
	return out
}
