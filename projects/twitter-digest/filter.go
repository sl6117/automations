package twitterdigest

import (
	"strings"

	"github.com/sl6117/automations/pkg/sources"
)

// filter -> drops low-signal posts before the LLM vets it (saves tokens)
// low engagement, exact duplicates, over maxPerAuthor
// per handle (0 = no cap). Feed order is newest-first, cap keeps each authors most recent posts
// also returns one observation row per fetched post, kept or dropped, in fetch order.
func filter(tweets []sources.Tweet, minEngagement, maxPerAuthor int) ([]sources.Tweet, []PostObservation) {
	seen := make(map[string]bool)
	perAuthor := make(map[string]int)
	out := make([]sources.Tweet, 0, len(tweets))
	obs := make([]PostObservation, 0, len(tweets))

	for _, tweet := range tweets {
		row := observe(tweet)
		key := normalize(tweet.Text)

		switch {
		case tweet.Likes+tweet.Reposts < minEngagement:
			row.DropReason = DropLowEngagement
		case seen[key]:
			// this is a duplicate
			row.DropReason = DropDuplicate
		case maxPerAuthor > 0 && perAuthor[tweet.Handle] >= maxPerAuthor:
			row.DropReason = DropPerAuthorCap
		default:
			row.Kept = true
			seen[key] = true
			perAuthor[tweet.Handle]++
			out = append(out, tweet)
		}
		obs = append(obs, row)
	}
	return out, obs
}

func normalize(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}
