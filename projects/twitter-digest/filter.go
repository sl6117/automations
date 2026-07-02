package twitterdigest

import (
	"strings"

	"github.com/sl6117/automations/pkg/sources"
)

// filter -> drops low-signal posts before the LLM vets it (saves tokens)
// low engagement, exact duplicates, over maxPerAuthor
// per handle (0 = no cap). Feed order is newest-first, cap keeps each authors most recent posts
func filter(tweets []sources.Tweet, minEngagement, maxPerAuthor int) []sources.Tweet {
	seen := make(map[string]bool)
	perAuthor := make(map[string]int)
	out := make([]sources.Tweet, 0, len(tweets))

	for _, tweet := range tweets {
		if tweet.Likes+tweet.Reposts < minEngagement {
			// not enough engagement to be worth summarizing
			continue
		}
		key := normalize(tweet.Text)

		if seen[key] {
			// this is a duplicate
			continue
		}
		if maxPerAuthor > 0 && perAuthor[tweet.Handle] >= maxPerAuthor {
			continue
		}
		seen[key] = true
		perAuthor[tweet.Handle]++
		out = append(out, tweet)
	}
	return out
}

func normalize(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}
