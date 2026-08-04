package weeklydeepdive

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sl6117/automations/internal/ai"
)

const (
	editorModel      = "claude-sonnet-4-5"
	editorMaxTokens  = 1500
	editorTemp       = 0.0
	editorSubmitTool = "submit_editor_report"
)

var editorReportSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
	  "pass": {"type": "boolean"},
	  "failures": {
		"type": "array",
		"items": {"type": "string"}
	  }
	},
	"required": ["pass", "failures"]
  }`)

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
		"Judge whether the brief respects the synthesizer contract given these research reports.\n\n%s\n\nCall %s with your verdict.",
		payload, editorSubmitTool,
	)
	resp, err := client.Chat(ctx, ai.ChatRequest{
		Model:    editorModel,
		System:   system,
		Messages: []ai.Message{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: prompt}}}},
		Tools: []ai.ToolDef{{
			Name:        editorSubmitTool,
			Description: "Submit the editor contract verdict for this brief",
			InputSchema: editorReportSchema,
		}},
		ToolChoice:  &ai.ToolChoice{Type: "tool", Name: editorSubmitTool},
		MaxTokens:   editorMaxTokens,
		Temperature: editorTemp,
	})
	if err != nil {
		return EditorReport{}, ai.Usage{}, fmt.Errorf("edit: %w", err)
	}
	if resp.StopReason == "max_tokens" {
		return EditorReport{}, resp.Usage, fmt.Errorf("edit: reply truncated by max_tokens")
	}
	if resp.StopReason != "tool_use" {
		preview := resp.Text
		if len(preview) > 200 {
			preview = preview[:200] + "…"
		}
		return EditorReport{}, resp.Usage, fmt.Errorf("edit: want stop_reason tool_use, got %q (preview: %q)", resp.StopReason, preview)
	}

	var input json.RawMessage
	for _, b := range resp.Content {
		if b.Type == "tool_use" && b.Name == editorSubmitTool {
			input = b.Input
			break
		}
	}
	if len(input) == 0 {
		return EditorReport{}, resp.Usage, fmt.Errorf("edit: no %s tool_use in response", editorSubmitTool)
	}
	report, err := parseEditorReport(input)
	if err != nil {
		preview := string(input)
		if len(preview) > 200 {
			preview = preview[:200] + "…"
		}
		return EditorReport{}, resp.Usage, fmt.Errorf("edit: %w (raw preview: %q)", err, preview)
	}
	return report, resp.Usage, nil
}

func parseEditorReport(raw json.RawMessage) (EditorReport, error) {

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
