package twitterdigest

import (
	"context"
	"fmt"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/pkg/sources"
)

// jevQuestions are the Sonnet judge's four dimensions, asked as yes/no.
// P(yes) means the dimension passes.
func jevQuestions() map[string]string {
	return map[string]string{
		"faithfulness": "Fail only if the digest changes a fact (number, name, date, tense, attribution, certainty, or scope), adds interpretation beyond one why-it-matters clause, or cites a claim to a tweet that does not contain it. Does faithfulness pass?",
		"topicRouting": "Fail only if a story sits under a clearly wrong section. A defensible placement passes. Does topic routing pass?",
		"coverage":     "Fail only if a clearly significant source tweet was omitted entirely. Minor, redundant, or content-free tweets may be omitted. Does coverage pass?",
		"clarity":      "Fail only for rambling, describing the same story twice, splitting one story across bullets, or a sentence that would confuse a reader. Does clarity pass?",
	}
}

func jevState(topics []Topic, kept []sources.Tweet, digest, language string) (string, error) {
	tweetsJSON, err := slimTweets(kept)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Language: %s\n\nAllowed topics:\n%s\n\nSource tweets:\n%s\n\nDigest:\n%s",
		language, topicList(topics), tweetsJSON, digest), nil
}

// jevShadow asks Jev for P(pass) on each dimension. The caller logs a failure and still delivers.
func jevShadow(ctx context.Context, client ai.Jev, topics []Topic, kept []sources.Tweet, digest, language string) (map[string]float64, error) {
	state, err := jevState(topics, kept, digest, language)
	if err != nil {
		return nil, err
	}
	return client.Nouls(ctx, state, jevQuestions())
}
