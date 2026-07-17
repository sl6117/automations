package twitterdigest

import (
	"context"
	"fmt"
	"log"
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

// runReviseLoop drives up to cfg.ReviseBudget revision passes when the judge failed faithfulness.
// Each pass revises the latest candidate against the latest critique and re-judges it.
// Returns the pair to ship: the first canddiate that re-judges clean on faithfulness, or the ORIGNIAL draft and report if no revision does.
// Never errors: any failure inside the loop means the original ships.
func runReviseLoop(ctx context.Context, client ai.Client, cfg Config, projectDir string, kept []sources.Tweet, draft string, report *JudgeReport, language string, logger *log.Logger) (string, *JudgeReport, ai.Usage) {
	var total ai.Usage
	candidate, candReport := draft, report

	for attempt := 1; attempt <= cfg.ReviseBudget && !candReport.Faithfulness.Pass; attempt++ {
		critique := "faithfulness: " + candReport.Faithfulness.Reason

		revised, rusage, err := reviseDigest(ctx, client, cfg.Model, projectDir, cfg.Topics, kept, candidate, critique, language)
		total.InputTokens += rusage.InputTokens
		total.OutputTokens += rusage.OutputTokens
		if err != nil {
			logger.Printf("[twitter-digest] revise attempt %d (%s): %v; keeping prior draft", attempt, language, err)
			break
		}

		rep, jusage, err := judgeDigest(ctx, client, cfg.judgeModel(), projectDir, cfg.Topics, kept, revised, language)
		total.InputTokens += jusage.InputTokens
		total.OutputTokens += jusage.OutputTokens
		if err != nil {
			logger.Printf("[twitter-digest] re-judge after revision %d (%s): %v; keeping prior draft", attempt, language, err)
			break
		}

		candidate, candReport = revised, &rep
		logger.Printf("[twitter-digest] revision %d (%s): faithfulness pass=%v", attempt, language, rep.Faithfulness.Pass)

	}

	// adopt-only-if-clean: an unverified or still-failing revision never ships
	if !candReport.Faithfulness.Pass {
		return draft, report, total
	}
	return candidate, candReport, total
}
