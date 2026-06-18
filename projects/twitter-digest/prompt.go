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

// buildPrompt loads prompts/digest.md and fills in the date, topics,
// and a slimmed-down JSON of the kept tweets. Only the fields the model needs are
// sent, to keep the prompt (and token cost) small.
func buildPrompt(projectDir string, topics []string, tweets []sources.Tweet) (string, error) {
	tmpl, err := os.ReadFile(filepath.Join(projectDir, "prompts", "digest.md"))

	if err != nil {
		return "", fmt.Errorf("read prompt: %w", err)
	}

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

	tweetsJSON, err := json.MarshalIndent(slims, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal tweets: %w", err)
	}

	out := string(tmpl)
	out = strings.ReplaceAll(out, "{{DATE}}", time.Now().Format("2006-01-02"))
	out = strings.ReplaceAll(out, "{{TOPICS}}", "- "+strings.Join(topics, "\n- "))
	out = strings.ReplaceAll(out, "{{TWEETS_JSON}}", string(tweetsJSON))

	return out, nil
}
