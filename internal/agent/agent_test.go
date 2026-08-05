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

// Server-side web_search is executed by Anthropic inside Chat. The loop must still offer
// it on the request, must not try to Call it locally, and must keep the server result
// blocks in the conversation when the model then asks for a client tool.
func TestRunOffersWebSearchAndOnlyCallsClientTools(t *testing.T) {
	searchBlock := ai.ContentBlock{
		Type: "web_search_tool_result",
		Raw:  json.RawMessage(`{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[{"type":"web_search_result","url":"https://example.com/a","encrypted_content":"enc"}]}`),
	}
	chat := &fakeChat{responses: []ai.ChatResponse{
		{
			StopReason: "tool_use",
			Content: []ai.ContentBlock{
				{Type: "server_tool_use", Raw: json.RawMessage(`{"type":"server_tool_use","id":"srv_1","name":"web_search","input":{"query":"q"}}`)},
				searchBlock,
				{Type: "tool_use", ID: "tu_1", Name: "fetch_url", Input: json.RawMessage(`{"url":"https://example.com/a"}`)},
			},
			Usage: ai.Usage{InputTokens: 10, OutputTokens: 5},
		},
		textResp(`{"question":"q","findings":["f"],"sources":["https://example.com/a"],"corroborated":true}`),
	}}
	tools := &fakeTools{result: `{"status":200,"body":"ok"}`}
	res, err := Run(context.Background(), Config{
		Client: chat, Tools: tools, Model: "m", MaxTokens: 100, MaxToolTurns: 3,
		WebSearchMaxUses: 3,
	}, "corroborate this")
	if err != nil {
		t.Fatal(err)
	}
	if res.ToolTurns != 1 {
		t.Errorf("ToolTurns = %d, want 1 (only the client tool counts)", res.ToolTurns)
	}
	if len(tools.calls) != 1 || !strings.Contains(tools.calls[0], "fetch_url") {
		t.Errorf("calls = %v, want only fetch_url — web_search is server-side", tools.calls)
	}
	if len(chat.requests[0].ServerTools) != 1 || chat.requests[0].ServerTools[0].MaxUses != 3 {
		t.Errorf("first request ServerTools = %+v, want web_search max_uses=3", chat.requests[0].ServerTools)
	}
	// continuation must still carry the search result so citations/allowlisting can see the URL
	assistant := chat.requests[1].Messages[1]
	if assistant.Role != "assistant" || len(assistant.Content) != 3 {
		t.Fatalf("continuation assistant = %+v, want the full 3-block turn", assistant)
	}
	if assistant.Content[1].Type != "web_search_tool_result" || !strings.Contains(string(assistant.Content[1].Raw), "enc") {
		t.Errorf("search block lost on continuation: %+v", assistant.Content[1])
	}
}

func TestRunWebSearchMaxUsesZeroOmitsServerTool(t *testing.T) {
	chat := &fakeChat{responses: []ai.ChatResponse{textResp("ok")}}
	tools := &fakeTools{}
	_, err := Run(context.Background(), Config{
		Client: chat, Tools: tools, Model: "m", MaxTokens: 100, MaxToolTurns: 1,
		WebSearchMaxUses: 0,
	}, "hi")
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.requests[0].ServerTools) != 0 {
		t.Errorf("ServerTools = %+v, want none when WebSearchMaxUses is 0", chat.requests[0].ServerTools)
	}
}

// PromptCache is opt-in: the agent loop forwards it on every Chat turn so
// Anthropic automatic caching can advance the breakpoint as the transcript grows.
func TestRunForwardsPromptCache(t *testing.T) {
	chat := &fakeChat{responses: []ai.ChatResponse{
		toolUseResp("tu_1", "list_runs", `{}`),
		textResp("done"),
	}}
	tools := &fakeTools{result: `{}`}
	res, err := Run(context.Background(), Config{
		Client: chat, Tools: tools, Model: "m", MaxTokens: 100, MaxToolTurns: 3,
		PromptCache: true,
	}, "which runs?")
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(chat.requests))
	}
	for i, req := range chat.requests {
		if !req.PromptCache {
			t.Errorf("request[%d].PromptCache = false, want true on every turn", i)
		}
	}
	// cache fields must accumulate across turns (scripted zeros here still prove the path)
	if res.Usage.CacheCreationInputTokens != 0 || res.Usage.CacheReadInputTokens != 0 {
		t.Errorf("usage cache fields = %+v", res.Usage)
	}
}

func TestRunOmitsPromptCacheWhenOff(t *testing.T) {
	chat := &fakeChat{responses: []ai.ChatResponse{textResp("ok")}}
	tools := &fakeTools{}
	_, err := Run(context.Background(), Config{
		Client: chat, Tools: tools, Model: "m", MaxTokens: 100, MaxToolTurns: 1,
	}, "hi")
	if err != nil {
		t.Fatal(err)
	}
	if chat.requests[0].PromptCache {
		t.Error("PromptCache = true, want false by default")
	}
}

func TestRunSumsCacheTokensAcrossTurns(t *testing.T) {
	chat := &fakeChat{responses: []ai.ChatResponse{
		{
			StopReason: "tool_use",
			Content:    []ai.ContentBlock{{Type: "tool_use", ID: "tu_1", Name: "list_runs", Input: json.RawMessage(`{}`)}},
			Usage:      ai.Usage{InputTokens: 10, OutputTokens: 5, CacheCreationInputTokens: 4000},
		},
		{
			StopReason: "end_turn",
			Content:    []ai.ContentBlock{{Type: "text", Text: "ok"}},
			Usage:      ai.Usage{InputTokens: 20, OutputTokens: 5, CacheReadInputTokens: 4010},
		},
	}}
	tools := &fakeTools{result: `{}`}
	res, err := Run(context.Background(), Config{
		Client: chat, Tools: tools, Model: "m", MaxTokens: 100, MaxToolTurns: 3,
		PromptCache: true,
	}, "which runs?")
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.InputTokens != 30 || res.Usage.CacheCreationInputTokens != 4000 || res.Usage.CacheReadInputTokens != 4010 {
		t.Errorf("usage = %+v, want in=30 creation=4000 read=4010", res.Usage)
	}
	if res.Usage.TotalInputTokens() != 8040 {
		t.Errorf("TotalInputTokens() = %d, want 8040", res.Usage.TotalInputTokens())
	}
}

func TestRunInvokesOnToolCall(t *testing.T) {
	chat := &fakeChat{responses: []ai.ChatResponse{
		toolUseResp("tu_1", "list_runs", `{"since":"2026-07-01"}`),
		textResp("ok"),
	}}
	tools := &fakeTools{result: `{"keys":["a"]}`}
	var gotName string
	var gotArgs, gotResult string
	var gotErr bool
	res, err := Run(context.Background(), Config{
		Client: chat, Tools: tools, Model: "m", MaxTokens: 100, MaxToolTurns: 3,
		OnToolCall: func(name string, args json.RawMessage, result string, isError bool) {
			gotName, gotArgs, gotResult, gotErr = name, string(args), result, isError
		},
	}, "which runs?")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "ok" {
		t.Fatalf("text = %q", res.Text)
	}
	if gotName != "list_runs" || gotArgs != `{"since":"2026-07-01"}` || gotResult != `{"keys":["a"]}` || gotErr {
		t.Errorf("OnToolCall got name=%q args=%q result=%q isError=%v", gotName, gotArgs, gotResult, gotErr)
	}
}

func planOutputTool() *ai.ToolDef {
	return &ai.ToolDef{
		Name:        "submit_plan",
		Description: "Submit the weekly deep-dive plan",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"story":{"type":"string"}},"required":["story"]}`),
	}
}

// OutputTool is a typed return channel: calling it ends the loop with its
// input as Result.Text and must not invoke ToolSource.Call.
func TestRunOutputToolEndsLoopWithoutCalling(t *testing.T) {
	planJSON := `{"story":"FIFA","whyChosen":"checkable","sourceTweetIDs":["1"],"researchQuestions":["q"]}`
	chat := &fakeChat{responses: []ai.ChatResponse{
		toolUseResp("tu_1", "list_runs", `{}`),
		toolUseResp("tu_2", "submit_plan", planJSON),
	}}
	tools := &fakeTools{result: `{"keys":["a"]}`}
	res, err := Run(context.Background(), Config{
		Client: chat, Tools: tools, Model: "m", MaxTokens: 100, MaxToolTurns: 3,
		OutputTool: planOutputTool(),
	}, "pick a story")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != planJSON || res.Truncated {
		t.Errorf("res = %+v, want plan JSON as Text, not truncated", res)
	}
	if len(tools.calls) != 1 || !strings.Contains(tools.calls[0], "list_runs") {
		t.Errorf("calls = %v, want only list_runs (submit_plan must not be Call'd)", tools.calls)
	}
	if res.ToolTurns != 1 {
		t.Errorf("ToolTurns = %d, want 1 (only the real tool turn)", res.ToolTurns)
	}
	// every turn must offer MCP tools + submit_plan
	for i, req := range chat.requests {
		if len(req.Tools) != 2 {
			t.Errorf("request[%d] tools = %d, want 2 (list_runs + submit_plan)", i, len(req.Tools))
		}
		if req.ToolChoice != nil {
			t.Errorf("request[%d] ToolChoice = %+v, want nil (auto) during research", i, req.ToolChoice)
		}
	}
}

func TestRunOutputToolRejectsEndTurn(t *testing.T) {
	chat := &fakeChat{responses: []ai.ChatResponse{
		textResp(`{"story":"x"}`),
	}}
	tools := &fakeTools{}
	_, err := Run(context.Background(), Config{
		Client: chat, Tools: tools, Model: "m", MaxTokens: 100, MaxToolTurns: 3,
		OutputTool: planOutputTool(),
	}, "pick a story")
	if err == nil || !strings.Contains(err.Error(), "submit_plan") {
		t.Fatalf("err = %v, want submit_plan complaint", err)
	}
	if len(tools.calls) != 0 {
		t.Errorf("calls = %v, want none", tools.calls)
	}
}

// Budget exhaustion with an OutputTool forces submit_plan rather than free-form text.
func TestRunOutputToolFinalAnswerForcesSubmit(t *testing.T) {
	planJSON := `{"story":"s","whyChosen":"w","sourceTweetIDs":[],"researchQuestions":["q"]}`
	chat := &fakeChat{responses: []ai.ChatResponse{
		toolUseResp("tu_1", "list_runs", `{}`),
		toolUseResp("tu_2", "list_runs", `{}`), // would be turn 2; budget is 1 → finalAnswer
		toolUseResp("tu_3", "submit_plan", planJSON),
	}}
	tools := &fakeTools{result: `{}`}
	res, err := Run(context.Background(), Config{
		Client: chat, Tools: tools, Model: "m", MaxTokens: 100, MaxToolTurns: 1,
		OutputTool: planOutputTool(),
	}, "pick a story")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || res.Text != planJSON {
		t.Errorf("res = %+v, want Truncated with submit_plan input", res)
	}
	if len(tools.calls) != 1 {
		t.Errorf("calls = %d, want 1", len(tools.calls))
	}
	final := chat.requests[2]
	if len(final.Tools) != 1 || final.Tools[0].Name != "submit_plan" {
		t.Errorf("final tools = %+v, want only submit_plan", final.Tools)
	}
	if final.ToolChoice == nil || final.ToolChoice.Type != "tool" || final.ToolChoice.Name != "submit_plan" {
		t.Errorf("final ToolChoice = %+v, want forced submit_plan", final.ToolChoice)
	}
}

// Calling the output tool on the turn that would hit the budget still succeeds
// (submit is not a research Call; it terminates before the budget gate).
func TestRunOutputToolBeatsBudgetGate(t *testing.T) {
	planJSON := `{"story":"s","whyChosen":"w","sourceTweetIDs":[],"researchQuestions":["q"]}`
	chat := &fakeChat{responses: []ai.ChatResponse{
		toolUseResp("tu_1", "list_runs", `{}`),
		toolUseResp("tu_2", "submit_plan", planJSON), // ToolTurns already 1, MaxToolTurns 1
	}}
	tools := &fakeTools{result: `{}`}
	res, err := Run(context.Background(), Config{
		Client: chat, Tools: tools, Model: "m", MaxTokens: 100, MaxToolTurns: 1,
		OutputTool: planOutputTool(),
	}, "pick a story")
	if err != nil {
		t.Fatal(err)
	}
	if res.Truncated || res.Text != planJSON {
		t.Errorf("res = %+v, want non-truncated plan from submit_plan", res)
	}
	if len(chat.requests) != 2 {
		t.Fatalf("requests = %d, want 2 (no finalAnswer)", len(chat.requests))
	}
}
