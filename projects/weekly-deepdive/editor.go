package weeklydeepdive

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sl6117/automations/internal/ai"
)

const (
	editorModel     = "claude-sonnet-4-5"
	editorMaxTokens = 1500
	editorTemp      = 0.0
)

// EditorReport is the editor contract gate. Failures are high-precision
// contract breaks only; an empty Failures list means pass.
type EditorReport struct {
	Pass     bool     `json:"pass"`
	Failures []string `json:"failures"`
}

func editBrief(ctx context.Context, client ai.ChatClient, system string, brief Brief, reports []ResearchReport) (EditorReport, ai.Usage, error) {
	payload, err := json.MarshalIndent(struct {
		HedgeLabel string           `json:"hedgeLabel"`
		Brief      Brief            `json:"brief"`
		Reports    []ResearchReport `json:"reports"`
	}{HedgeLabel: HedgeLabel, Brief: brief, Reports: reports}, "", "  ")
	if err != nil {
		return EditorReport{}, ai.Usage{}, err
	}
	prompt := fmt.Sprintf(
		"Judge whether the brief respects the synthesizer contract given these research reports.\n\n%s\n\nReply with ONLY a JSON object matching the schema in the system prompt.",
		payload,
	)
	resp, err := client.Chat(ctx, ai.ChatRequest{
		Model:       editorModel,
		System:      system,
		Messages:    []ai.Message{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: prompt}}}},
		MaxTokens:   editorMaxTokens,
		Temperature: editorTemp,
	})
	if err != nil {
		return EditorReport{}, ai.Usage{}, fmt.Errorf("edit: %w", err)
	}
	if resp.StopReason == "max_tokens" {
		return EditorReport{}, resp.Usage, fmt.Errorf("edit: reply truncated by max_tokens")
	}
	if resp.StopReason == "max_tokens" {
		return EditorReport{}, resp.Usage, fmt.Errorf("edit: reply truncated by max_tokens")
	}
	report, err := parseEditorReport(resp.Text)
	if err != nil {
		preview := resp.Text
		if len(preview) > 200 {
			preview = preview[:200] + "…"
		}
		return EditorReport{}, resp.Usage, fmt.Errorf("edit: %w (raw preview: %q)", err, preview)
	}
	return report, resp.Usage, nil
}

func parseEditorReport(text string) (EditorReport, error) {
	raw, err := extractJSON(text)
	if err != nil {
		return EditorReport{}, err
	}
	var got struct {
		Pass     *bool     `json:"pass"`
		Failures *[]string `json:"failures"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		return EditorReport{}, fmt.Errorf("parse editor report: %w", err)
	}
	if got.Pass == nil || got.Failures == nil {
		return EditorReport{}, fmt.Errorf("editor report missing required fields")
	}
	if *got.Pass && len(*got.Failures) > 0 {
		return EditorReport{}, fmt.Errorf("editor report inconsistent: pass=true with non-empty failures")
	}
	if !*got.Pass && len(*got.Failures) == 0 {
		return EditorReport{}, fmt.Errorf("editor report inconsistent: pass=false with empty failures")
	}
	return EditorReport{Pass: *got.Pass, Failures: *got.Failures}, nil
}
