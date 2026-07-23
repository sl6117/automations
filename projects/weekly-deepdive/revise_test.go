package weeklydeepdive

import (
	"context"
	"fmt"
	"io"
	"log"
	"testing"

	"github.com/sl6117/automations/internal/ai"
)

// scriptedChat returns canned replies in order; fails the test if over-called.
type scriptedChat struct {
	t       *testing.T
	replies []string
	calls   int
}

func (s *scriptedChat) Chat(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	if s.calls >= len(s.replies) {
		s.t.Fatalf("unexpected chat call %d", s.calls+1)
	}
	reply := s.replies[s.calls]
	s.calls++
	return ai.ChatResponse{StopReason: "end_turn", Text: reply}, nil
}

type errChat struct{ calls int }

func (e *errChat) Chat(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	e.calls++
	return ai.ChatResponse{}, fmt.Errorf("api down")
}

var (
	testLogger  = log.New(io.Discard, "", 0)
	origBrief   = Brief{Title: "Original", Summary: "s", Sections: []BriefSection{{Heading: "h", Body: "b"}}}
	failReport  = EditorReport{Pass: false, Failures: []string{"claim missing hedge label"}}
	revisedJSON = `{"title":"Revised","summary":"s","sections":[{"heading":"h","body":"b"}]}`
)

func TestReviseLoopAdoptsCleanRevision(t *testing.T) {
	chat := &scriptedChat{t: t, replies: []string{revisedJSON, `{"pass":true,"failures":[]}`}}
	brief, report, _ := runReviseLoop(context.Background(), chat, "synth", "edit", Plan{}, nil, origBrief, failReport, 1, testLogger)
	if brief.Title != "Revised" || !report.Pass {
		t.Fatalf("want revised brief adopted, got title=%q pass=%v", brief.Title, report.Pass)
	}
	if chat.calls != 2 {
		t.Fatalf("want 2 chat calls (revise + re-edit), got %d", chat.calls)
	}
}
func TestReviseLoopKeepsOriginalWhenStillFailing(t *testing.T) {
	chat := &scriptedChat{t: t, replies: []string{revisedJSON, `{"pass":false,"failures":["still bad"]}`}}
	brief, report, _ := runReviseLoop(context.Background(), chat, "synth", "edit", Plan{}, nil, origBrief, failReport, 1, testLogger)
	if brief.Title != "Original" {
		t.Fatalf("still-failing revision must not ship; got %q", brief.Title)
	}
	if len(report.Failures) != 1 || report.Failures[0] != "claim missing hedge label" {
		t.Fatalf("want original report kept, got %+v", report)
	}
}
func TestReviseLoopFailsOpenOnError(t *testing.T) {
	chat := &errChat{}
	brief, report, _ := runReviseLoop(context.Background(), chat, "synth", "edit", Plan{}, nil, origBrief, failReport, 1, testLogger)
	if brief.Title != "Original" || report.Pass {
		t.Fatalf("API error must keep original, got title=%q pass=%v", brief.Title, report.Pass)
	}
	if chat.calls != 1 {
		t.Fatalf("want exactly 1 attempted call, got %d", chat.calls)
	}
}
func TestReviseLoopBudgetZeroIsNoop(t *testing.T) {
	chat := &scriptedChat{t: t, replies: nil}
	brief, report, usage := runReviseLoop(context.Background(), chat, "synth", "edit", Plan{}, nil, origBrief, failReport, 0, testLogger)
	if brief.Title != "Original" || report.Pass || chat.calls != 0 || usage.InputTokens != 0 {
		t.Fatal("budget 0 must not call the model or change anything")
	}
}
