package weeklydeepdive

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/ai"
)

func TestFilterReplanQuestions(t *testing.T) {
	asked := []string{"Q1", "Q2"}
	cases := []struct {
		name     string
		proposed []string
		cap      int
		want     []string
	}{
		{name: "dedup exact", proposed: []string{"Q1", "Q3"}, cap: 2, want: []string{"Q3"}},
		{name: "cap", proposed: []string{"Q3", "Q4", "Q5"}, cap: 2, want: []string{"Q3", "Q4"}},
		{name: "trim blanks", proposed: []string{"  ", "Q3", ""}, cap: 2, want: []string{"Q3"}},
		{name: "cap zero", proposed: []string{"Q3"}, cap: 0, want: nil},
		{name: "all dupes", proposed: []string{"Q1", "Q2"}, cap: 2, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := filterReplanQuestions(asked, tc.proposed, tc.cap)
			if len(got) != len(tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("got %#v, want %#v", got, tc.want)
				}
			}
		})
	}
}

func TestParseReplan(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr string
		check   func(t *testing.T, r Replan)
	}{
		{
			name: "happy path with questions",
			in: `{
				"newResearchQuestions": ["Did X happen by date Y?"],
				"rationale": "Editor flagged uncorroborated timeline claim"
			}`,
			check: func(t *testing.T, r Replan) {
				if len(r.NewResearchQuestions) != 1 || r.NewResearchQuestions[0] == "" {
					t.Errorf("questions = %#v", r.NewResearchQuestions)
				}
				if r.Rationale == "" {
					t.Error("empty rationale")
				}
			},
		},
		{
			name: "empty questions ok",
			in: `{
				"newResearchQuestions": [],
				"rationale": "Failures are hedge/label only; revise the brief"
			}`,
			check: func(t *testing.T, r Replan) {
				if r.NewResearchQuestions == nil {
					t.Fatal("want non-nil empty slice, got nil")
				}
				if len(r.NewResearchQuestions) != 0 {
					t.Errorf("questions = %#v, want empty", r.NewResearchQuestions)
				}
			},
		},
		{
			name:    "missing questions field",
			in:      `{"rationale":"x"}`,
			wantErr: "missing required fields",
		},
		{
			name:    "missing rationale field",
			in:      `{"newResearchQuestions":[]}`,
			wantErr: "missing required fields",
		},
		{
			name:    "empty rationale",
			in:      `{"newResearchQuestions":[],"rationale":"  "}`,
			wantErr: "rationale must be non-empty",
		},
		{
			name:    "invalid json",
			in:      `{`,
			wantErr: "parse replan",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseReplan(json.RawMessage(tc.in))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

func TestReplanOnceUsesForcedSubmitTool(t *testing.T) {
	chat := &captureReplanChat{input: `{"newResearchQuestions":["q1"],"rationale":"gap"}`}
	got, usage, err := replanOnce(
		context.Background(), chat, "system",
		"story",
		[]string{"already asked"},
		[]ResearchReport{{Question: "already asked", Findings: []string{"f"}, Sources: []string{"s"}, Corroborated: true}},
		[]string{"missing corroboration on price impact"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.NewResearchQuestions) != 1 || got.NewResearchQuestions[0] != "q1" {
		t.Errorf("replan = %+v", got)
	}
	if usage.InputTokens != 11 || usage.OutputTokens != 3 {
		t.Errorf("usage = %+v, want 11/3 from the fake", usage)
	}
	if chat.got.Model != replanModel {
		t.Errorf("Model = %q, want %q", chat.got.Model, replanModel)
	}
	if chat.got.ToolChoice == nil || chat.got.ToolChoice.Type != "tool" || chat.got.ToolChoice.Name != replanSubmitTool {
		t.Errorf("ToolChoice = %+v, want forced %s", chat.got.ToolChoice, replanSubmitTool)
	}
	if len(chat.got.Tools) != 1 || chat.got.Tools[0].Name != replanSubmitTool {
		t.Errorf("Tools = %+v, want only %s", chat.got.Tools, replanSubmitTool)
	}
	if !strings.Contains(string(chat.got.Tools[0].InputSchema), `"newResearchQuestions"`) {
		t.Errorf("InputSchema = %s", chat.got.Tools[0].InputSchema)
	}
	prompt := chat.got.Messages[0].Content[0].Text
	for _, want := range []string{"story", "already asked", "missing corroboration", replanSubmitTool} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q: %q", want, prompt)
		}
	}
}

func TestReplanOnceRejectsEndTurn(t *testing.T) {
	chat := &endTurnReplanChat{}
	_, _, err := replanOnce(context.Background(), chat, "system", "s", nil, nil, []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "tool_use") {
		t.Fatalf("err = %v, want tool_use complaint", err)
	}
}

func TestReplanConfigHelpers(t *testing.T) {
	if got := (Config{}).replanRounds(); got != 0 {
		t.Fatalf("default rounds = %d, want 0 (off)", got)
	}
	if got := (Config{ReplanBudget: 1}).replanRounds(); got != 1 {
		t.Fatalf("rounds = %d, want 1", got)
	}
	if got := (Config{}).replanQuestionCap(); got != 0 {
		t.Fatalf("default cap = %d, want 0 (off)", got)
	}
	if got := (Config{MaxReplanQuestions: 2}).replanQuestionCap(); got != 2 {
		t.Fatalf("cap = %d, want 2", got)
	}
}

type captureReplanChat struct {
	input string
	got   ai.ChatRequest
}

func (c *captureReplanChat) Chat(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	c.got = req
	return ai.ChatResponse{
		StopReason: "tool_use",
		Content: []ai.ContentBlock{{
			Type:  "tool_use",
			ID:    "tu_replan",
			Name:  replanSubmitTool,
			Input: json.RawMessage(c.input),
		}},
		Usage: ai.Usage{InputTokens: 11, OutputTokens: 3},
	}, nil
}

type endTurnReplanChat struct{}

func (endTurnReplanChat) Chat(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	return ai.ChatResponse{
		StopReason: "end_turn",
		Text:       `{"newResearchQuestions":[],"rationale":"x"}`,
		Content:    []ai.ContentBlock{{Type: "text", Text: `{"newResearchQuestions":[],"rationale":"x"}`}},
	}, nil
}
