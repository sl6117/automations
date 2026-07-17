package twitterdigest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/pkg/sources"
)

// buildRevisePrompt assembles the revision prompt from the same slim tweet JSON
// the generator and judge saw - all three share one view of the ground truth
func buildRevisePrompt(projectDir string, topics []Topic, kept []sources.Tweet, digest, critique, language string) (string, error) {
	tmpl, err := os.ReadFile(filepath.Join(projectDir, "prompts", "revise.md"))
	if err != nil {
		return "", fmt.Errorf("read revise prompt: %w", err)
	}

	tweetsJSON, err := slimTweets(kept)
	if err != nil {
		return "", fmt.Errorf("marshal kept tweets: %w", err)
	}

	out := string(tmpl)
	out = strings.ReplaceAll(out, "{{LANGUAGE}}", language)
	out = strings.ReplaceAll(out, "{{TOPICS}}", topicList(topics))
	out = strings.ReplaceAll(out, "{{TWEETS_JSON}}", string(tweetsJSON))
	out = strings.ReplaceAll(out, "{{DIGEST}}", digest)
	out = strings.ReplaceAll(out, "{{CRITIQUE}}", critique)
	return out, nil
}

// reviseDigest asks the generator model for one revision pass driven by the judge's
// critique. Callers own the loop policy: budget, re-judging, and whether to adopt
// the result. An error here must never block delivery.
func reviseDigest(ctx context.Context, client ai.Client, model, projectDir string, topics []Topic, kept []sources.Tweet, digest, critique, language string) (string, ai.Usage, error) {
	prompt, err := buildRevisePrompt(projectDir, topics, kept, digest, critique, language)
	if err != nil {
		return "", ai.Usage{}, err
	}

	resp, err := client.Complete(ctx, ai.Request{
		Model:       model,
		Prompt:      prompt,
		MaxTokens:   digestMaxTokens,
		Temperature: digestTemperature,
	})
	if err != nil {
		return "", resp.Usage, fmt.Errorf("revise via %s: %w", model, err)
	}
	return resp.Text, resp.Usage, nil
}
