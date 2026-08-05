package weeklydeepdive

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/ai"
)

func TestParseBrief(t *testing.T) {
	valid := `{
		"title": "OpenRouter shift",
		"summary": "Chinese models lead US firm usage on OpenRouter.",
		"sections": [
			{"heading": "What we know", "body": "Usage hit 58% (reported but not corroborated)."}
		]
	}`
	cases := []struct {
		name    string
		in      string
		wantErr string
	}{
		{name: "happy path", in: valid},
		{name: "missing sections", in: `{"title":"t","summary":"s"}`, wantErr: "missing required fields"},
		{name: "empty sections", in: `{"title":"t","summary":"s","sections":[]}`, wantErr: "sections must be non-empty"},
		{name: "empty section body", in: `{"title":"t","summary":"s","sections":[{"heading":"h","body":"  "}]}`, wantErr: "heading/body must be non-empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseBrief(json.RawMessage(tc.in))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Title == "" || len(got.Sections) == 0 {
				t.Errorf("brief = %+v", got)
			}
			if !strings.Contains(got.Sections[0].Body, HedgeLabel) {
				t.Errorf("body should include hedge label %q", HedgeLabel)
			}
		})
	}
}

func TestSynthesizeUsesForcedSubmitTool(t *testing.T) {
	input := `{"title":"t","summary":"s","sections":[{"heading":"h","body":"b"}]}`
	chat := &captureSynthChat{input: input}
	brief, usage, err := synthesize(context.Background(), chat, "claude-haiku-4-5", "system", Plan{Story: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if brief.Title != "t" || len(brief.Sections) != 1 {
		t.Errorf("brief = %+v", brief)
	}
	if usage.InputTokens != 7 || usage.OutputTokens != 2 {
		t.Errorf("usage = %+v, want 7/2", usage)
	}
	assertForcedBriefTool(t, chat.got)
}

func TestReviseBriefUsesForcedSubmitTool(t *testing.T) {
	input := `{"title":"Revised","summary":"s","sections":[{"heading":"h","body":"b"}]}`
	chat := &captureSynthChat{input: input}
	brief, _, err := reviseBrief(context.Background(), chat, "system", Plan{}, nil, Brief{Title: "Old"}, []string{"missing hedge"})
	if err != nil {
		t.Fatal(err)
	}
	if brief.Title != "Revised" {
		t.Errorf("title = %q, want Revised", brief.Title)
	}
	assertForcedBriefTool(t, chat.got)
	if strings.Contains(chat.got.Messages[0].Content[0].Text, "ONLY a JSON") {
		t.Errorf("revise prompt still asks for prose JSON: %q", chat.got.Messages[0].Content[0].Text)
	}
}

func TestSynthesizeRejectsEndTurn(t *testing.T) {
	_, _, err := synthesize(context.Background(), endTurnSynthChat{}, "m", "s", Plan{}, nil)
	if err == nil || !strings.Contains(err.Error(), "tool_use") {
		t.Fatalf("err = %v, want tool_use complaint", err)
	}
}

func assertForcedBriefTool(t *testing.T, got ai.ChatRequest) {
	t.Helper()
	if got.ToolChoice == nil || got.ToolChoice.Type != "tool" || got.ToolChoice.Name != synthSubmitTool {
		t.Errorf("ToolChoice = %+v, want forced %s", got.ToolChoice, synthSubmitTool)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != synthSubmitTool {
		t.Errorf("Tools = %+v, want only %s", got.Tools, synthSubmitTool)
	}
	if !strings.Contains(string(got.Tools[0].InputSchema), `"sections"`) {
		t.Errorf("InputSchema = %s, want brief schema", got.Tools[0].InputSchema)
	}
}

type captureSynthChat struct {
	input string
	got   ai.ChatRequest
}

func (c *captureSynthChat) Chat(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	c.got = req
	return ai.ChatResponse{
		StopReason: "tool_use",
		Content: []ai.ContentBlock{{
			Type:  "tool_use",
			ID:    "tu_synth",
			Name:  synthSubmitTool,
			Input: json.RawMessage(c.input),
		}},
		Usage: ai.Usage{InputTokens: 7, OutputTokens: 2},
	}, nil
}

type endTurnSynthChat struct{}

func (endTurnSynthChat) Chat(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	return ai.ChatResponse{
		StopReason: "end_turn",
		Text:       `{"title":"t","summary":"s","sections":[{"heading":"h","body":"b"}]}`,
		Content: []ai.ContentBlock{{
			Type: "text",
			Text: `{"title":"t","summary":"s","sections":[{"heading":"h","body":"b"}]}`,
		}},
	}, nil
}
