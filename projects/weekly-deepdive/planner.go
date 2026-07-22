package weeklydeepdive

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sl6117/automations/internal/agent"
)

// Plan is the planner role's contract. Downstream roles trust these fields, not prose.
type Plan struct {
	Story             string   `json:"story"`
	WhyChosen         string   `json:"whyChosen"`
	SourceTweetIDs    []string `json:"sourceTweetIDs"`
	ResearchQuestions []string `json:"researchQuestions"`
}

func planWeek(ctx context.Context, cfg agent.Config, now time.Time) (Plan, agent.Result, error) {
	since := now.UTC().AddDate(0, 0, -7).Format("2006-01-02")
	prompt := fmt.Sprintf(
		"Pick the single biggest story from digests since %s (rolling 7 days). Use the tools. Reply with ONLY a JSON object matching the schema in the system prompt.",
		since,
	)
	res, err := agent.Run(ctx, cfg, prompt)
	if err != nil {
		return Plan{}, res, err
	}
	plan, err := parsePlan(res.Text)
	return plan, res, err
}

func parsePlan(text string) (Plan, error) {
	raw, err := extractJSON(text)
	if err != nil {
		return Plan{}, err
	}
	// pointers: missing field = error, not silent zero
	var got struct {
		Story             *string   `json:"story"`
		WhyChosen         *string   `json:"whyChosen"`
		SourceTweetIDs    *[]string `json:"sourceTweetIDs"`
		ResearchQuestions *[]string `json:"researchQuestions"`
	}

	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		return Plan{}, fmt.Errorf("parse plan: %w", err)
	}
	if got.Story == nil || got.WhyChosen == nil || got.SourceTweetIDs == nil || got.ResearchQuestions == nil {
		return Plan{}, fmt.Errorf("plan missing required fields")
	}
	if strings.TrimSpace(*got.Story) == "" || strings.TrimSpace(*got.WhyChosen) == "" {
		return Plan{}, fmt.Errorf("plan story/whyChosen must be non-empty")
	}
	if len(*got.ResearchQuestions) == 0 {
		return Plan{}, fmt.Errorf("plan researchQuestions must be non-empty")
	}
	return Plan{
		Story:             *got.Story,
		WhyChosen:         *got.WhyChosen,
		SourceTweetIDs:    *got.SourceTweetIDs,
		ResearchQuestions: *got.ResearchQuestions,
	}, nil
}

func extractJSON(text string) (string, error) {
	start := strings.Index(text, "{")
	if start == -1 {
		return "", fmt.Errorf("no JSON found")
	}
	var raw json.RawMessage
	if err := json.NewDecoder(strings.NewReader(text[start:])).Decode(&raw); err != nil {
		return "", fmt.Errorf("decode JSON: %w", err)
	}
	return string(raw), nil
}
