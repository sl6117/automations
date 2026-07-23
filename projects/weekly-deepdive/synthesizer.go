package weeklydeepdive

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sl6117/automations/internal/ai"
)

// HedgeLabel must appear  inline on any claim not backed by a corrroborated finding.
// The editor gate checks for this string; keep it stable.
const HedgeLabel = "reported but not corroborated"

// Brief is the synthesizer contract. Downstream editor trusts structure + hedge labels, not vibes.
type Brief struct {
	Title    string         `json:"title"`
	Summary  string         `json:"summary"`
	Sections []BriefSection `json:"sections"`
}

type BriefSection struct {
	Heading string `json:"heading"`
	Body    string `json:"body"`
}

func synthesize(ctx context.Context, client ai.ChatClient, model, system string, plan Plan, reports []ResearchReport) (Brief, ai.Usage, error) {
	payload, err := json.MarshalIndent(struct {
		Plan    Plan             `json:"plan"`
		Reports []ResearchReport `json:"reports"`
	}{Plan: plan, Reports: reports}, "", "  ")
	if err != nil {
		return Brief{}, ai.Usage{}, err
	}
	prompt := fmt.Sprintf(
		"Write the weekly deep-dive brief from this plan and research reports.\n\n%s\n\nReply with ONLY a JSON object matching the schema in the system prompt.",
		payload,
	)
	resp, err := client.Chat(ctx, ai.ChatRequest{
		Model:     model,
		System:    system,
		Messages:  []ai.Message{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: prompt}}}},
		MaxTokens: synthesizerMaxTokens,
	})
	if err != nil {
		return Brief{}, ai.Usage{}, fmt.Errorf("synthesize: %w", err)
	}
	if resp.StopReason == "max_tokens" {
		return Brief{}, resp.Usage, fmt.Errorf("synthesize: reply truncated by max_tokens")
	}
	brief, err := parseBrief(resp.Text)
	if err != nil {
		preview := resp.Text
		if len(preview) > 200 {
			preview = preview[:200] + "…"
		}
		return Brief{}, resp.Usage, fmt.Errorf("synthesize: %w (raw preview: %q)", err, preview)
	}
	return brief, resp.Usage, err
}

func parseBrief(text string) (Brief, error) {
	raw, err := extractJSON(text)
	if err != nil {
		return Brief{}, err
	}
	var got struct {
		Title    *string         `json:"title"`
		Summary  *string         `json:"summary"`
		Sections *[]BriefSection `json:"sections"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		return Brief{}, fmt.Errorf("parse brief: %w", err)
	}
	if got.Title == nil || got.Summary == nil || got.Sections == nil {
		return Brief{}, fmt.Errorf("brief missing required fields")
	}
	if strings.TrimSpace(*got.Title) == "" || strings.TrimSpace(*got.Summary) == "" {
		return Brief{}, fmt.Errorf("brief title/summary must be non-empty")
	}
	if len(*got.Sections) == 0 {
		return Brief{}, fmt.Errorf("brief sections must be non-empty")
	}
	for i, s := range *got.Sections {
		if strings.TrimSpace(s.Heading) == "" || strings.TrimSpace(s.Body) == "" {
			return Brief{}, fmt.Errorf("brief section %d heading/body must be non-empty", i)
		}
	}
	return Brief{Title: *got.Title, Summary: *got.Summary, Sections: *got.Sections}, nil
}
