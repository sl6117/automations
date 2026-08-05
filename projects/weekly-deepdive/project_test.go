package weeklydeepdive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"testing"
	"time"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/internal/obs"
	"github.com/sl6117/automations/internal/runner"
	"github.com/sl6117/automations/internal/storage"
	"github.com/sl6117/automations/pkg/sinks"
)

// pipelineChat plays back canned responses in order: planner, researcher(s),
// synthesizer, editor. Unlike revise_test's scriptedChat it carries usage and
// content blocks, which the agent loop and the cost log both need.
// A call past the end of the script is a test failure surfaced as a chat error.
type pipelineChat struct {
	responses []ai.ChatResponse
	calls     int
}

func (s *pipelineChat) Chat(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	if s.calls >= len(s.responses) {
		return ai.ChatResponse{}, fmt.Errorf("unexpected chat call %d (script has %d)", s.calls+1, len(s.responses))
	}
	resp := s.responses[s.calls]
	s.calls++
	return resp, nil
}

// textResponse is an end_turn reply whose whole content is one text block,
// with distinct token counts so the test can verify usage is summed.
func textResponse(text string, in, out int) ai.ChatResponse {
	return ai.ChatResponse{
		StopReason: "end_turn",
		Text:       text,
		Content:    []ai.ContentBlock{{Type: "text", Text: text}},
		Usage:      ai.Usage{InputTokens: in, OutputTokens: out},
	}
}

// toolSubmitResponse is a forced-tool reply: stop_reason tool_use with one
// tool_use block. Used for the editor (and later other schema-forced roles).
func toolSubmitResponse(name, input string, in, out int) ai.ChatResponse {
	return ai.ChatResponse{
		StopReason: "tool_use",
		Content: []ai.ContentBlock{{
			Type:  "tool_use",
			ID:    "tu_1",
			Name:  name,
			Input: json.RawMessage(input),
		}},
		Usage: ai.Usage{InputTokens: in, OutputTokens: out},
	}
}

// pipelineTools serves the digest-archive reads seedTweets makes. No agent
// in the script ever requests a tool call, so Call outside these two names fails.
type pipelineTools struct{}

func (pipelineTools) Tools(ctx context.Context) ([]ai.ToolDef, error) {
	return []ai.ToolDef{{Name: "fetch_url", InputSchema: json.RawMessage(`{"type":"object"}`)}}, nil
}

func (pipelineTools) Call(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	switch name {
	case "list_runs":
		return `{"keys":["logs/runs/a.json"]}`, false, nil
	case "get_artifact":
		return `{"artifact":{"kept":[{"ID":"1","Handle":"alice","Text":"breaking https://example.com/story","URL":"https://x.com/alice/status/1"}]}}`, false, nil
	default:
		return "", false, fmt.Errorf("unexpected tool call %q", name)
	}
}

type captureSink struct {
	messages []string
}

func (c *captureSink) Name() string { return "capture" }
func (c *captureSink) Deliver(ctx context.Context, message string) error {
	c.messages = append(c.messages, message)
	return nil
}

// pipelineScript is a full happy-path conversation: a one-question plan,
// one researcher report, a brief, and a passing editor verdict (so the
// revise loop stays off despite reviseBudget=1 in config.json).
func pipelineScript() []ai.ChatResponse {
	plan := `{
		"story": "FIFA adds a year without a source",
		"whyChosen": "Checkable claim",
		"sourceTweetIDs": ["1"],
		"researchQuestions": ["What did the source actually say?"]
	}`
	report := `{
		"question": "What did the source actually say?",
		"findings": ["The linked article confirms the date change."],
		"sources": ["https://example.com/story"],
		"corroborated": true
	}`
	brief := `{
		"title": "FIFA's extra year",
		"summary": "The date change is confirmed by the linked article.",
		"sections": [{"heading": "What happened", "body": "The article confirms it."}]
	}`
	editor := `{"pass": true, "failures": []}`
	return []ai.ChatResponse{
		toolSubmitResponse(plannerSubmitTool, plan, 100, 10),
		toolSubmitResponse(researcherSubmitTool, report, 200, 20),
		toolSubmitResponse(synthSubmitTool, brief, 300, 30),
		toolSubmitResponse(editorSubmitTool, editor, 400, 40),
	}
}

func pipelineRuntime(t *testing.T) *runner.Runtime {
	t.Helper()
	// lesson learned #2: never let ambient env point tests at real storage
	t.Setenv("AUTOMATION_ROOT", t.TempDir())
	return &runner.Runtime{
		DryRun:     false,
		Log:        log.New(io.Discard, "", 0),
		ProjectDir: ".",
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
}

func TestRunLogsCostForWholePipeline(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}
	sink := &captureSink{}
	p := &project{
		chat:  &pipelineChat{responses: pipelineScript()},
		tools: pipelineTools{},
		now:   fixedNow,
		sinks: []sinks.Sink{sink},
		store: store,
	}

	if err := p.Run(context.Background(), pipelineRuntime(t)); err != nil {
		t.Fatal(err)
	}
	if len(sink.messages) != 1 {
		t.Fatalf("delivered %d messages, want 1", len(sink.messages))
	}

	data, err := store.Get(context.Background(), obs.CostLogKey)
	if err != nil {
		t.Fatalf("read cost log: %v", err)
	}
	var run obs.Run
	if err := json.Unmarshal(data, &run); err != nil {
		t.Fatalf("parse cost log line %q: %v", data, err)
	}
	if run.Project != "weekly-deepdive" {
		t.Errorf("project = %q, want weekly-deepdive", run.Project)
	}
	if run.Model != "claude-haiku-4-5" {
		t.Errorf("model = %q, want claude-haiku-4-5 (must stay a key of obs.Prices)", run.Model)
	}
	// planner 100/10 + researcher 200/20 + synthesizer 300/30 + editor 400/40
	if run.InputTokens != 1000 || run.OutputTokens != 100 {
		t.Errorf("tokens = %d in / %d out, want 1000 / 100 (all four roles summed)", run.InputTokens, run.OutputTokens)
	}
	if run.ItemCount != 1 {
		t.Errorf("itemCount = %d, want 1 research report", run.ItemCount)
	}
	if run.DryRun {
		t.Error("dryRun = true, want false")
	}
	if run.CostUSD <= 0 {
		t.Errorf("costUsd = %v, want > 0 on a live run", run.CostUSD)
	}
}

// When the editor fails and the revise loop runs, its tokens are real spend
// and must land in the cost log too. Script: the four pipeline roles with a
// failing editor verdict, then the revise pass (re-synthesize + re-edit).
func TestRunCostIncludesReviseLoopTokens(t *testing.T) {
	script := pipelineScript()
	script[3] = toolSubmitResponse(editorSubmitTool, `{"pass": false, "failures": ["claim missing hedge label"]}`, 400, 40)
	revised := `{
		"title": "FIFA's extra year, hedged",
		"summary": "The date change is confirmed by the linked article.",
		"sections": [{"heading": "What happened", "body": "The article confirms it."}]
	}`
	script = append(script,
		toolSubmitResponse(synthSubmitTool, revised, 500, 50),
		toolSubmitResponse(editorSubmitTool, `{"pass": true, "failures": []}`, 600, 60),
	)

	store := &storage.FS{Root: t.TempDir()}
	p := &project{
		chat:  &pipelineChat{responses: script},
		tools: pipelineTools{},
		now:   fixedNow,
		sinks: []sinks.Sink{&captureSink{}},
		store: store,
	}
	if err := p.Run(context.Background(), pipelineRuntime(t)); err != nil {
		t.Fatal(err)
	}

	data, err := store.Get(context.Background(), obs.CostLogKey)
	if err != nil {
		t.Fatalf("read cost log: %v", err)
	}
	var run obs.Run
	if err := json.Unmarshal(data, &run); err != nil {
		t.Fatalf("parse cost log line %q: %v", data, err)
	}
	// base pipeline 1000/100 + revise 500/50 + re-edit 600/60
	if run.InputTokens != 2100 || run.OutputTokens != 210 {
		t.Errorf("tokens = %d in / %d out, want 2100 / 210 (revise loop tokens must be counted)", run.InputTokens, run.OutputTokens)
	}
}

// brokenStore fails every write: observability must never cost a delivered brief.
type brokenStore struct{}

func (brokenStore) Get(ctx context.Context, key string) ([]byte, error) {
	return nil, storage.ErrNotExist
}
func (brokenStore) Put(ctx context.Context, key string, data []byte) error {
	return errors.New("store down")
}
func (brokenStore) Append(ctx context.Context, key string, line []byte) error {
	return errors.New("store down")
}
func (brokenStore) List(ctx context.Context, prefix string) ([]string, error) {
	return nil, nil
}

func TestRunSucceedsWhenCostLogWriteFails(t *testing.T) {
	sink := &captureSink{}
	p := &project{
		chat:  &pipelineChat{responses: pipelineScript()},
		tools: pipelineTools{},
		now:   fixedNow,
		sinks: []sinks.Sink{sink},
		store: brokenStore{},
	}

	if err := p.Run(context.Background(), pipelineRuntime(t)); err != nil {
		t.Fatalf("run must not fail after delivery just because the cost log write failed: %v", err)
	}
	if len(sink.messages) != 1 {
		t.Fatalf("delivered %d messages, want 1", len(sink.messages))
	}
}
