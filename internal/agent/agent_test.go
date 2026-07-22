package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/ai"
)

// fakeChat replays scripted responses and records every request it saw.
type fakeChat struct {
	responses []ai.ChatResponse
	requests  []ai.ChatRequest
}

func (f *fakeChat) Chat(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	f.requests = append(f.requests, req)
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

type fakeTools struct {
	calls   []string
	result  string
	isError bool
}

func (f *fakeTools) Tools(ctx context.Context) ([]ai.ToolDef, error) {
	return []ai.ToolDef{{Name: "list_runs", InputSchema: json.RawMessage(`{"type":"object"}`)}}, nil
}
func (f *fakeTools) Call(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	f.calls = append(f.calls, name+" "+string(args))
	return f.result, f.isError, nil
}
func toolUseResp(id, name, input string) ai.ChatResponse {
	return ai.ChatResponse{
		StopReason: "tool_use",
		Content:    []ai.ContentBlock{{Type: "tool_use", ID: id, Name: name, Input: json.RawMessage(input)}},
		Usage:      ai.Usage{InputTokens: 10, OutputTokens: 5},
	}
}
func textResp(text string) ai.ChatResponse {
	return ai.ChatResponse{
		StopReason: "end_turn",
		Content:    []ai.ContentBlock{{Type: "text", Text: text}},
		Usage:      ai.Usage{InputTokens: 10, OutputTokens: 5},
	}
}
func TestRunExecutesToolsAndReturnsAnswer(t *testing.T) {
	chat := &fakeChat{responses: []ai.ChatResponse{
		toolUseResp("tu_1", "list_runs", `{"since":"2026-07-14"}`),
		textResp("three runs this week"),
	}}
	tools := &fakeTools{result: `{"keys":["a","b","c"]}`}
	res, err := Run(context.Background(), Config{Client: chat, Tools: tools, Model: "m", MaxTokens: 100, MaxToolTurns: 3}, "which runs?")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "three runs this week" || res.Truncated || res.ToolTurns != 1 {
		t.Errorf("res = %+v, want the final text, not truncated, 1 tool turn", res)
	}
	if len(tools.calls) != 1 || !strings.Contains(tools.calls[0], "2026-07-14") {
		t.Errorf("tool calls = %v, want one list_runs call with the model's args", tools.calls)
	}
	if res.Usage.InputTokens != 20 || res.Usage.OutputTokens != 10 {
		t.Errorf("usage = %+v, want sums across both turns", res.Usage)
	}
	// the second request must carry the tool result back, matched by id
	second := chat.requests[1]
	last := second.Messages[len(second.Messages)-1]
	if last.Role != "user" || last.Content[0].Type != "tool_result" || last.Content[0].ToolUseID != "tu_1" {
		t.Errorf("second request's last message = %+v, want a tool_result for tu_1", last)
	}
	if !strings.Contains(last.Content[0].Content, `"keys"`) {
		t.Errorf("tool_result content = %q, want the tool output", last.Content[0].Content)
	}
}
func TestRunBudgetExhaustionTruncatesWithLabel(t *testing.T) {
	chat := &fakeChat{responses: []ai.ChatResponse{
		toolUseResp("tu_1", "list_runs", `{}`),
		toolUseResp("tu_2", "list_runs", `{}`), // wants more, but budget is 1
		textResp("partial answer from one call"),
	}}
	tools := &fakeTools{result: "{}"}
	res, err := Run(context.Background(), Config{Client: chat, Tools: tools, Model: "m", MaxTokens: 100, MaxToolTurns: 1}, "which runs?")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || res.Text != "partial answer from one call" {
		t.Errorf("res = %+v, want Truncated with the forced final text", res)
	}
	if len(tools.calls) != 1 {
		t.Errorf("tool calls = %d, want 1: the second request must not execute", len(tools.calls))
	}
	final := chat.requests[2]
	if len(final.Tools) != 0 {
		t.Errorf("final request offered %d tools, want none - the model must not be able to ask again", len(final.Tools))
	}
	last := final.Messages[len(final.Messages)-1]
	if last.Content[0].Type != "tool_result" || !last.Content[0].IsError || last.Content[0].ToolUseID != "tu_2" {
		t.Errorf("pending tu_2 = %+v, want a budget-exhausted error tool_result", last.Content[0])
	}
}
