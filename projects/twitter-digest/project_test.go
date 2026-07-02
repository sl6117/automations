package twitterdigest

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/internal/runner"
	"github.com/sl6117/automations/pkg/sinks"
	"github.com/sl6117/automations/pkg/sources"
)

type fakeSink struct {
	delivered []string
}

func (f *fakeSink) Name() string { return "fake" }

func (f *fakeSink) Deliver(ctx context.Context, message string) error {
	f.delivered = append(f.delivered, message)
	return nil
}

type quietSource struct{}

func (quietSource) Name() string { return "quiet" }

func (quietSource) Fetch(ctx context.Context) ([]sources.Tweet, error) {
	return []sources.Tweet{
		{ID: "99", Author: "spam bot", Handle: "@cryptomoonboy", Text: "BUY $SCAM", Likes: 2, Reposts: 0},
	}, nil
}

func TestProjectRun(t *testing.T) {
	t.Setenv("AUTOMATION_ROOT", t.TempDir())
	var buf bytes.Buffer
	runTime := &runner.Runtime{
		DryRun:     true,
		Log:        log.New(&buf, "", 0),
		ProjectDir: ".",
	}

	if err := (&project{source: sources.Mock{}}).Run(context.Background(), runTime); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	got := buf.String()

	for _, want := range []string{"Daily X Digest", "## AI", "Dario"} {
		if !strings.Contains(got, want) {
			t.Errorf("digest missing %q\n--- output ---\n%s", want, got)
		}
	}

	if strings.Contains(got, "@cryptomoonboy") {
		t.Errorf("digest contains spam handle\n--- output ---\n%s", got)
	}

}

type fakeClient struct {
	resp   ai.Response
	gotReq ai.Request
}

func (f *fakeClient) Complete(ctx context.Context, req ai.Request) (ai.Response, error) {
	f.gotReq = req
	return f.resp, nil
}

func TestProjectRunLLM(t *testing.T) {
	t.Setenv("AUTOMATION_ROOT", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "test-key")

	fake := &fakeClient{resp: ai.Response{
		Text:  "## AI\n- Dario says AI is coming",
		Model: "anthropic/claude-haiku-4.5",
		Usage: ai.Usage{InputTokens: 10, OutputTokens: 20},
	}}

	sink := &fakeSink{}

	var buf bytes.Buffer
	runTime := &runner.Runtime{DryRun: false, Log: log.New(&buf, "", 0), ProjectDir: "."}

	p := &project{client: fake, source: sources.Mock{}, sinks: []sinks.Sink{sink}}

	if err := p.Run(context.Background(), runTime); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if len(sink.delivered) != 1 || !strings.Contains(sink.delivered[0], "Dario says AI is coming") {
		t.Errorf("sink deliveries = %#v, want one message with model text", sink.delivered)
	}

	if fake.gotReq.Model != "claude-haiku-4-5" {
		t.Errorf("model = %q, want config model", fake.gotReq.Model)
	}

	state, err := loadState()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state.SinceID != "6" {
		t.Errorf("cursor = %q, want 6 (newest mock tweet id)", state.SinceID)
	}
}

func TestProjectRunSkipsWhenNothingKept(t *testing.T) {
	t.Setenv("AUTOMATION_ROOT", t.TempDir())

	fake := &fakeClient{}
	sink := &fakeSink{}

	var buf bytes.Buffer
	runTime := &runner.Runtime{DryRun: false, Log: log.New(&buf, "", 0), ProjectDir: "."}

	p := &project{client: fake, source: quietSource{}, sinks: []sinks.Sink{sink}}

	if err := p.Run(context.Background(), runTime); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if fake.gotReq.Prompt != "" {
		t.Errorf("LLM was called even though nothing was kept")
	}
	if len(sink.delivered) != 0 {
		t.Errorf("delivered %d messages, want 0", len(sink.delivered))
	}

	state, err := loadState()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}

	if state.SinceID != "99" {
		t.Errorf("cursor = %q, want 99 (newest quiet source tweet id)", state.SinceID)
	}
}
