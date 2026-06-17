package twitterdigest

import (
	"strings"

	"github.com/sl6117/automations/pkg/sources"
)

// Bucket is one topic section of the digest
type Bucket struct {
	Topic  string
	Tweets []sources.Tweet
}

// Digest is the full result: topic buckets in the configured order
// with a trailing "other" bucket for anything unmatched
type Digest struct {
	Buckets []Bucket
}

// summarize groups tweets into topic buckets. This is the DRY-RUN heuristic:
// a tweet joins the 1st topic whose name appears in the tweet text.
// unmatched tweets go into the "otherBucket" bucket.
// No LLM/ no tokens
// the real model replaces this with smarter categorization + short summaries
func summarize(tweets []sources.Tweet, topics []string) Digest {
	buckets := make([]Bucket, len(topics))

	// initialize all buckets
	for i, topic := range topics {
		buckets[i] = Bucket{Topic: topic}
	}

	otherBucket := Bucket{Topic: "Other"}

	for _, t := range tweets {
		text := strings.ToLower(t.Text)
		matched := false

		for i, topic := range topics {
			if strings.Contains(text, strings.ToLower(topic)) {
				buckets[i].Tweets = append(buckets[i].Tweets, t)
				matched = true
				break
			}
		}
		if !matched {
			otherBucket.Tweets = append(otherBucket.Tweets, t)
		}
	}

	out := make([]Bucket, 0, len(buckets)+1)
	for _, b := range buckets {
		if len(b.Tweets) > 0 {
			out = append(out, b)
		}
	}
	if len(otherBucket.Tweets) > 0 {
		out = append(out, otherBucket)
	}
	return Digest{Buckets: out}
}
