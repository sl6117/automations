package twitterdigest

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/internal/runner"
	"github.com/sl6117/automations/internal/storage"
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

	state, err := loadState(context.Background(), storage.NewFS())
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

	state, err := loadState(context.Background(), storage.NewFS())
	if err != nil {
		t.Fatalf("load state: %v", err)
	}

	if state.SinceID != "99" {
		t.Errorf("cursor = %q, want 99 (newest quiet source tweet id)", state.SinceID)
	}
}

func TestProjectRunRoutesSubscribers(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOMATION_ROOT", root)
	dir := filepath.Join(root, "projects", "twitter-digest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	subsJSON := `[
		{"name": "alice", "sink": "console", "topics": ["AI"]},
		{"name": "bob", "sink": "console", "topics": ["*"]}
	]`

	if err := os.WriteFile(filepath.Join(dir, "subscribers.json"), []byte(subsJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := &fakeClient{resp: ai.Response{
		Text:  "## AI\n- ai story\n\n## Other\n- misc story",
		Model: "claude-haiku-4-5",
		Usage: ai.Usage{InputTokens: 10, OutputTokens: 20},
	}}

	got := map[string]*fakeSink{}

	p := &project{
		client: fake,
		source: sources.Mock{},
		sinkFor: func(sub Subscriber, cfg Config, rt *runner.Runtime) (sinks.Sink, error) {
			s := &fakeSink{}
			got[sub.Name] = s
			return s, nil
		},
	}

	var buf bytes.Buffer
	runTime := &runner.Runtime{DryRun: false, Log: log.New(&buf, "", 0), ProjectDir: "."}

	if err := p.Run(context.Background(), runTime); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	alice := got["alice"]
	if alice == nil || len(alice.delivered) != 1 ||
		!strings.Contains(alice.delivered[0], "ai story") ||
		strings.Contains(alice.delivered[0], "misc story") {
		t.Errorf("alice (AI only) got wrong digest: %#v", alice)
	}
	bob := got["bob"]
	if bob == nil || len(bob.delivered) != 1 ||
		!strings.Contains(bob.delivered[0], "ai story") ||
		!strings.Contains(bob.delivered[0], "misc story") {
		t.Errorf("bob (wildcard) got wrong digest: %#v", bob)
	}
}

type langClient struct{ prompts []string }

func (m *langClient) Complete(ctx context.Context, req ai.Request) (ai.Response, error) {
	m.prompts = append(m.prompts, req.Prompt)
	text := "## AI\n- english story"

	if strings.Contains(req.Prompt, "Korean") {
		text = "## AI\n- korean story"
	}
	return ai.Response{Text: text, Model: "claude-haiku-4-5", Usage: ai.Usage{InputTokens: 10, OutputTokens: 20}}, nil
}

func TestProjectRunPerLanguageDigests(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOMATION_ROOT", root)
	dir := filepath.Join(root, "projects", "twitter-digest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	subsJSON := `[
		{"name": "alice", "sink": "console", "topics": ["AI"]},
		{"name": "hana", "sink": "console", "topics": ["AI"], "language": "Korean"}
	]`
	if err := os.WriteFile(filepath.Join(dir, "subscribers.json"), []byte(subsJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &langClient{}
	got := map[string]*fakeSink{}
	p := &project{
		client: fake,
		source: sources.Mock{},
		sinkFor: func(sub Subscriber, cfg Config, rt *runner.Runtime) (sinks.Sink, error) {
			s := &fakeSink{}
			got[sub.Name] = s
			return s, nil
		},
	}
	var buf bytes.Buffer
	runTime := &runner.Runtime{DryRun: false, Log: log.New(&buf, "", 0), ProjectDir: "."}
	if err := p.Run(context.Background(), runTime); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(fake.prompts) != 2 {
		t.Errorf("LLM called %d times, want 2 (one per language)", len(fake.prompts))
	}
	if a := got["alice"]; a == nil || len(a.delivered) != 1 || !strings.Contains(a.delivered[0], "english story") {
		t.Errorf("alice got wrong digest: %#v", a)
	}
	if h := got["hana"]; h == nil || len(h.delivered) != 1 || !strings.Contains(h.delivered[0], "korean story") {
		t.Errorf("hana got wrong digest: %#v", h)
	}
}
