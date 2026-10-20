package weeklydeepdive

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/ai"
)

func TestParseEditorReport(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr string
		pass    bool
	}{
		{
			name: "pass",
			in:   `{"pass":true,"failures":[]}`,
			pass: true,
		},
		{
			name: "fail with reasons",
			in:   `{"pass":false,"failures":["asserted Qeshm strike without hedge"]}`,
		},
		{
			name:    "missing failures",
			in:      `{"pass":true}`,
			wantErr: "missing required fields",
		},
		{
			name:    "pass with failures",
			in:      `{"pass":true,"failures":["x"]}`,
			wantErr: "inconsistent",
		},
		{
			name:    "fail with empty failures",
			in:      `{"pass":false,"failures":[]}`,
			wantErr: "inconsistent",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseEditorReport(json.RawMessage(tc.in))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Pass != tc.pass {
				t.Errorf("Pass = %v, want %v", got.Pass, tc.pass)
			}
		})
	}
}

// editBrief must force submit_editor_report and parse tool_use.input — not prose JSON.
func TestEditBriefUsesForcedSubmitTool(t *testing.T) {
	chat := &captureEditChat{input: `{"pass":true,"failures":[]}`}
	report, usage, err := editBrief(context.Background(), chat, "system", Brief{Title: "t"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Pass || len(report.Failures) != 0 {
		t.Errorf("report = %+v, want pass with empty failures", report)
	}
	if usage.InputTokens != 11 || usage.OutputTokens != 3 {
		t.Errorf("usage = %+v, want 11/3 from the fake", usage)
	}
	if chat.got.ToolChoice == nil || chat.got.ToolChoice.Type != "auto" || chat.got.ToolChoice.Name != "" {
		t.Errorf("ToolChoice = %+v, want auto with no name", chat.got.ToolChoice)
	}
	if !chat.got.Tools[0].Strict {
		t.Errorf("editor tool Strict = false, want true")
	}
	if chat.got.Thinking != "between_tools" {
		t.Errorf("Thinking = %q, want between_tools", chat.got.Thinking)
	}
	if !strings.Contains(string(chat.got.Tools[0].InputSchema), `"additionalProperties": false`) {
		t.Errorf("InputSchema missing additionalProperties: false: %s", chat.got.Tools[0].InputSchema)
	}
	if strings.Contains(chat.got.Messages[0].Content[0].Text, "ONLY a JSON") {
		t.Errorf("user prompt still asks for prose JSON: %q", chat.got.Messages[0].Content[0].Text)
	}
}

func TestEditBriefRejectsEndTurn(t *testing.T) {
	chat := &endTurnEditChat{}
	_, _, err := editBrief(context.Background(), chat, "system", Brief{Title: "t"}, nil)
	if err == nil || !strings.Contains(err.Error(), "tool_use") {
		t.Fatalf("err = %v, want tool_use complaint", err)
	}
}

// captureEditChat records the request and returns a forced-tool reply.
type captureEditChat struct {
	input string
	got   ai.ChatRequest
}

func (c *captureEditChat) Chat(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	c.got = req
	return ai.ChatResponse{
		StopReason: "tool_use",
		Content: []ai.ContentBlock{{
			Type:  "tool_use",
			ID:    "tu_edit",
			Name:  editorSubmitTool,
			Input: json.RawMessage(c.input),
		}},
		Usage: ai.Usage{InputTokens: 11, OutputTokens: 3},
	}, nil
}

type endTurnEditChat struct{}

func (endTurnEditChat) Chat(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	return ai.ChatResponse{
		StopReason: "end_turn",
		Text:       `{"pass":true,"failures":[]}`,
		Content:    []ai.ContentBlock{{Type: "text", Text: `{"pass":true,"failures":[]}`}},
	}, nil
}
