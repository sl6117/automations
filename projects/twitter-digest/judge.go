package twitterdigest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/pkg/sources"
)

const (
	judgeTemperature = 0.0
	judgeMaxTokens   = 500
)

type Verdict struct {
	Pass   bool   `json:"pass"`
	Reason string `json:"reason"`
}

type JudgeReport struct {
	Faithfulness Verdict `json:"faithfulness"`
	TopicRouting Verdict `json:"topicRouting"`
	Coverage     Verdict `json:"coverage"`
	Clarity      Verdict `json:"clarity"`
}

// buildJudgePrompt assembles the evaluator prompt from the same slim tweet
// JSON the digest writer saw - the judge and the generator must share one view of the ground truth
func buildJudgePrompt(projectDir string, topics []Topic, kept []sources.Tweet, digest, language string) (string, error) {
	tmpl, err := os.ReadFile(filepath.Join(projectDir, "prompts", "judge.md"))
	if err != nil {
		return "", fmt.Errorf("read judge prompt: %w", err)
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

	return out, nil
}

// extractJSON returns the outermost JSON object in text, tolerating the markdown fences
// and stray commentary small models sometimes add despite being told not to.
func extractJSON(text string) (string, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end <= start {
		return "", fmt.Errorf("no JSON found")
	}
	return text[start : end+1], nil
}

// judgeDigest asks the model to grade a rendered digest against its source tweets
// Observability only: callers log and store the report; a judge error or failing verdict
// must never block delivery.
func judgeDigest(ctx context.Context, client ai.Client, model, projectDir string, topics []Topic, kept []sources.Tweet, digest, language string) (JudgeReport, ai.Usage, error) {
	var report JudgeReport

	prompt, err := buildJudgePrompt(projectDir, topics, kept, digest, language)
	if err != nil {
		return report, ai.Usage{}, err
	}

	resp, err := client.Complete(ctx, ai.Request{
		Model:       model,
		Prompt:      prompt,
		MaxTokens:   judgeMaxTokens,
		Temperature: judgeTemperature,
	})
	if err != nil {
		return report, ai.Usage{}, fmt.Errorf("judge via %s: %w", model, err)
	}

	raw, err := extractJSON(resp.Text)
	if err != nil {
		return report, resp.Usage, err
	}

	// unmarshal through pointers so a missing dimension is a load error,
	// not a silent zero-value fail verdict with no reason
	var got struct {
		Faithfulness *Verdict `json:"faithfulness"`
		TopicRouting *Verdict `json:"topicRouting"`
		Coverage     *Verdict `json:"coverage"`
		Clarity      *Verdict `json:"clarity"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		return report, resp.Usage, fmt.Errorf("parse judge response: %w", err)
	}
	if got.Faithfulness == nil || got.TopicRouting == nil || got.Coverage == nil || got.Clarity == nil {
		return report, resp.Usage, fmt.Errorf("judge response missing dimensions")
	}
	report = JudgeReport{
		Faithfulness: *got.Faithfulness,
		TopicRouting: *got.TopicRouting,
		Coverage:     *got.Coverage,
		Clarity:      *got.Clarity,
	}
	return report, resp.Usage, nil
}

// Failures lists the failing dimensions with ther reasons
// empty when pass
func (r JudgeReport) Failures() []string {
	var out []string
	for _, d := range []struct {
		name string
		v    Verdict
	}{
		{"faithfulness", r.Faithfulness},
		{"topicRouting", r.TopicRouting},
		{"coverage", r.Coverage},
		{"clarity", r.Clarity},
	} {
		if !d.v.Pass {
			out = append(out, d.name+": "+d.v.Reason)
		}
	}
	return out
}
