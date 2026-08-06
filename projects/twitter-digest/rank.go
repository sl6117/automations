package twitterdigest

import (
	"sort"
	"time"
)

// RankParams: add-only ranking-budget filter.
// Phase 1 = absolute floor (minEngagement) + caps — never displaced.
// Phase 2 = relative-only posts fill spare digestBudget / per-author slots.
type RankParams struct {
	MinEngagement int
	RelativeK     float64
	MaxPerAuthor  int
	DigestBudget  int
	Alpha         float64
	RunAt         time.Time
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

func applyRankedFilter(obs []PostObservation, medians map[string]int, p RankParams) []PostObservation {
	out := make([]PostObservation, len(obs))
	copy(out, obs)
	for i := range out {
		out[i].Kept = false
		out[i].DropReason = ""
	}
	eligible := make(map[string][]int)
	for i := range out {
		row := &out[i]
		switch {
		case !row.IsRetweet && row.OwnTextLength == 0:
			row.DropReason = DropEmptyOwnText
		case row.engagement() < p.MinEngagement:
			// leave for phase 2 (relative); reason set later if still not kept
		default:
			row.Kept = true
			eligible[row.Handle] = append(eligible[row.Handle], i)
		}
	}
	applyScoreCaps(out, eligible, p)
	authorKept := map[string]int{}
	keptN := 0
	for _, row := range out {
		if row.Kept {
			keptN++
			authorKept[row.Handle]++
		}
	}
	var relative []int
	for i := range out {
		row := &out[i]
		if row.Kept || row.DropReason == DropEmptyOwnText {
			continue
		}
		// Phase-1 cap losers stay out — do not revive them as "relative"
		if row.DropReason == DropPerAuthorCap || row.DropReason == DropDigestBudget {
			continue
		}
		if !clearsRelative(row.engagement(), row.IsRetweet, medians[row.Handle], p.RelativeK) {
			row.DropReason = DropLowEngagement
			continue
		}
		relative = append(relative, i)
	}
	sort.SliceStable(relative, func(a, b int) bool {
		x, y := out[relative[a]], out[relative[b]]
		if x.IsRetweet != y.IsRetweet {
			return !x.IsRetweet
		}
		return x.score() > y.score()
	})
	for _, i := range relative {
		if p.DigestBudget > 0 && keptN >= p.DigestBudget {
			out[i].DropReason = DropDigestBudget
			continue
		}
		h := out[i].Handle
		if p.MaxPerAuthor > 0 && authorKept[h] >= p.MaxPerAuthor {
			out[i].DropReason = DropPerAuthorCap
			continue
		}
		out[i].Kept = true
		out[i].DropReason = ""
		keptN++
		authorKept[h]++
	}
	return out
}
func clearsRelative(eng int, isRetweet bool, handleMedian int, relativeK float64) bool {
	if isRetweet || handleMedian < 1 || relativeK <= 0 {
		return false
	}
	return float64(eng) >= relativeK*float64(handleMedian)
}

// clearsHybridFloor is the keep gate before ranking. RTs cannot use the relatice path
// - their engagement is the original's audience, not this handle's.
func clearsHybridFloor(eng int, isRetweet bool, handleMedian, minEngagement int, relativeK float64) bool {
	if eng >= minEngagement {
		return true
	}
	if isRetweet || handleMedian < 1 || relativeK <= 0 {
		return false
	}
	return float64(eng) >= relativeK*float64(handleMedian)
}

// applyScoreCaps trims a provisional kept set by maxPerAuthor then digestBudget using score().
func applyScoreCaps(out []PostObservation, eligible map[string][]int, p RankParams) {
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
				return x.score() > y.score()
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
				return out[keptIdx[a]].score() > out[keptIdx[b]].score()
			})
			for _, i := range keptIdx[p.DigestBudget:] {
				out[i].Kept = false
				out[i].DropReason = DropDigestBudget
			}
		}
	}
}

// applyAddOnly seeds phase 1 from archive Kept flags (includes production dedup),
// then fills spare digestBudget / per-author slots from low_engagement + relative clears.
func applyAddOnly(obs []PostObservation, medians map[string]int, p RankParams) []PostObservation {
	out := make([]PostObservation, len(obs))
	copy(out, obs)
	authorKept := map[string]int{}
	keptN := 0
	for _, row := range out {
		if row.Kept {
			keptN++
			authorKept[row.Handle]++
		}
	}
	var relative []int
	for i := range out {
		if out[i].Kept {
			continue
		}
		if out[i].DropReason != DropLowEngagement {
			continue
		}
		if !clearsRelative(out[i].engagement(), out[i].IsRetweet, medians[out[i].Handle], p.RelativeK) {
			continue
		}
		relative = append(relative, i)
	}
	sort.SliceStable(relative, func(a, b int) bool {
		return out[relative[a]].score() > out[relative[b]].score()
	})
	for _, i := range relative {
		if p.DigestBudget > 0 && keptN >= p.DigestBudget {
			break
		}
		h := out[i].Handle
		if p.MaxPerAuthor > 0 && authorKept[h] >= p.MaxPerAuthor {
			continue
		}
		out[i].Kept = true
		out[i].DropReason = ""
		keptN++
		authorKept[h]++
	}
	return out
}
