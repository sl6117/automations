package twitterdigest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sl6117/automations/pkg/sources"
)

// // buildPrompt loads prompts/digest.md into a stable system prefix (date, topics, tweets)
// // and per-languate user directive. The system prefix is identical across EN/KO/RU
// // so Anthropic prompt caching can hit within one run.
func buildPrompt(projectDir string, topics []Topic, tweets []sources.Tweet, language string) (system, user string, err error) {
	tmpl, err := os.ReadFile(filepath.Join(projectDir, "prompts", "digest.md"))

	if err != nil {
		return "", "", fmt.Errorf("read prompt: %w", err)
	}

	tweetsJSON, err := slimTweets(tweets)
	if err != nil {
		return "", "", fmt.Errorf("marshal tweets: %w", err)
	}

	system = string(tmpl)
	system = strings.ReplaceAll(system, "{{DATE}}", time.Now().Format("2006-01-02"))
	system = strings.ReplaceAll(system, "{{TOPICS}}", topicList(topics))
	system = strings.ReplaceAll(system, "{{TWEETS_JSON}}", string(tweetsJSON))

	user = fmt.Sprintf(
		"Write every summary in %s, regardless of the post's language. Keep proper nouns as commonly romanized.",
		language,
	)

	return system, user, nil
}

// slimTweets renders the model-facing view of tweets: only the fields
// prompts need, so tokens aren't spent on engagement metadata.
func slimTweets(tweets []sources.Tweet) ([]byte, error) {
	type slim struct {
		Author string `json:"author"`
		Handle string `json:"handle"`
		URL    string `json:"url"`
		Text   string `json:"text"`
	}
	slims := make([]slim, len(tweets))
	for i, t := range tweets {
		slims[i] = slim{Author: t.Author, Handle: t.Handle, Text: t.Text, URL: t.URL}
	}
	return json.MarshalIndent(slims, "", "  ")
}

// topicList formats the allowed topics as prompt-ready bullet lines.
func topicList(topics []Topic) string {
	topicLines := make([]string, len(topics))

	for i, topic := range topics {
		topicLines[i] = "- " + topic.Name
		if topic.Description != "" {
			topicLines[i] += ": " + topic.Description
		}
	}
	return strings.Join(topicLines, "\n")
}
