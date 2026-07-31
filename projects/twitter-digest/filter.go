package twitterdigest

import (
	"sort"
	"strings"

	"github.com/sl6117/automations/pkg/sources"
)

// filter -> drops low-signal posts before the LLM vets it (saves tokens)
// low engagement, exact duplicates, over maxPerAuthor
// per handle (0 = no cap). Eligible posts are ranked by engagement within their author, so
// the cap keeps each author's best posts rather than whichever arrived first. Retweets rank
// below the author's own posts: X reports them with zero likes and the original's repost
// count, so their engagement is someone else's audience, not this author's.
// also returns one observation row per fetched post, kept or dropped, in fetch order.
func filter(tweets []sources.Tweet, minEngagement, maxPerAuthor, digestBudget int) ([]sources.Tweet, []PostObservation) {
	seen := make(map[string]bool)
	eligible := make(map[string][]int)
	obs := make([]PostObservation, 0, len(tweets))

	for _, tweet := range tweets {
		row := observe(tweet)
		key := normalize(tweet.Text)

		switch {
		case !row.IsRetweet && row.OwnTextLength == 0:
			row.DropReason = DropEmptyOwnText
		case tweet.Likes+tweet.Reposts < minEngagement:
			row.DropReason = DropLowEngagement
		case seen[key]:
			row.DropReason = DropDuplicate
		default:
			row.Kept = true
			seen[key] = true
			eligible[tweet.Handle] = append(eligible[tweet.Handle], len(obs))
		}
		obs = append(obs, row)
	}
	if maxPerAuthor > 0 {
		for _, idx := range eligible {
			if len(idx) <= maxPerAuthor {
				continue
			}
			// stable, so equal engagement leaves the newer post ahead as feed order had it
			sort.SliceStable(idx, func(a, b int) bool {
				x, y := obs[idx[a]], obs[idx[b]]
				if x.IsRetweet != y.IsRetweet {
					return !x.IsRetweet
				}
				return x.score() > y.score()
			})
			for _, i := range idx[maxPerAuthor:] {
				obs[i].Kept = false
				obs[i].DropReason = DropPerAuthorCap
			}
		}
	}
	if digestBudget > 0 {
		var keptIdx []int
		for i, row := range obs {
			if row.Kept {
				keptIdx = append(keptIdx, i)
			}
		}
		if len(keptIdx) > digestBudget {
			sort.SliceStable(keptIdx, func(a, b int) bool {
				return obs[keptIdx[a]].score() > obs[keptIdx[b]].score()
			})
			for _, i := range keptIdx[digestBudget:] {
				obs[i].Kept = false
				obs[i].DropReason = DropDigestBudget
			}
		}

	}

	out := make([]sources.Tweet, 0, len(obs))
	for i, row := range obs {
		if row.Kept {
			out = append(out, tweets[i])
		}
	}

	return out, obs
}

func normalize(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}
