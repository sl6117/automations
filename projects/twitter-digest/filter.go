package twitterdigest

import (
	"strings"

	"github.com/sl6117/automations/pkg/sources"
)

// filter -> drops low-signal posts before the LLM vets it (saves tokens)
func filter(tweets []sources.Tweet, minEngagement int) []sources.Tweet {
	seen := make(map[string]bool)
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
		seen[key] = true
		out = append(out, tweet)
	}
	return out
}

func normalize(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}
