package twitterdigest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/internal/queue"
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

type quotaSource struct{}

func (quotaSource) Name() string { return "quota" }

func (quotaSource) Fetch(ctx context.Context) ([]sources.Tweet, error) {
	return nil, fmt.Errorf("x api 403: %w", sources.ErrQuota)
}

func TestQuotaFetchFailureAlertsOperatorAndFailsRun(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOMATION_ROOT", root)
	alert := &fakeSink{}
	p := &project{source: quotaSource{}, store: &storage.FS{Root: root}, alert: alert}

	var buf bytes.Buffer
	runTime := &runner.Runtime{DryRun: false, Log: log.New(&buf, "", 0), ProjectDir: "."}

	if err := p.Run(context.Background(), runTime); err == nil {
		t.Fatal("a quota failure must still fail the run - there is nothing to deliver")
	}
	if len(alert.delivered) != 1 || !strings.Contains(alert.delivered[0], "spend cap") {
		t.Errorf("operator alert = %#v, want one spend-cap notice", alert.delivered)
	}
}

func (quietSource) Name() string { return "quiet" }

func (quietSource) Fetch(ctx context.Context) ([]sources.Tweet, error) {
	return []sources.Tweet{
		{ID: "99", Author: "spam bot", Handle: "@cryptomoonboy", Text: "BUY $SCAM", Likes: 2, Reposts: 0},
	}, nil
}

func TestProjectRun(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOMATION_ROOT", root)
	var buf bytes.Buffer
	runTime := &runner.Runtime{
		DryRun:     true,
		Log:        log.New(&buf, "", 0),
		ProjectDir: ".",
	}

	if err := (&project{source: sources.Mock{}, store: &storage.FS{Root: root}}).Run(context.Background(), runTime); err != nil {
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
	resp    ai.Response
	gotReq  ai.Request
	gotReqs []ai.Request
}

func (f *fakeClient) Complete(ctx context.Context, req ai.Request) (ai.Response, error) {
	f.gotReq = req
	f.gotReqs = append(f.gotReqs, req)
	return f.resp, nil
}

func TestProjectRunLLM(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOMATION_ROOT", root)
	t.Setenv("OPENROUTER_API_KEY", "test-key")

	fake := &fakeClient{resp: ai.Response{
		Text:  "## AI\n- Dario says AI is coming",
		Model: "anthropic/claude-haiku-4.5",
		Usage: ai.Usage{InputTokens: 10, OutputTokens: 20},
	}}

	sink := &fakeSink{}

	var buf bytes.Buffer
	runTime := &runner.Runtime{DryRun: false, Log: log.New(&buf, "", 0), ProjectDir: "."}

	store := &storage.FS{Root: root}
	p := &project{client: fake, source: sources.Mock{}, sinks: []sinks.Sink{sink}, store: store}

	if err := p.Run(context.Background(), runTime); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if len(sink.delivered) != 1 || !strings.Contains(sink.delivered[0], "Dario says AI is coming") {
		t.Errorf("sink deliveries = %#v, want one message with model text", sink.delivered)
	}

	if fake.gotReqs[0].Model != "claude-haiku-4-5" {
		t.Errorf("model = %q, want config model", fake.gotReqs[0].Model)
	}
	if last := fake.gotReqs[len(fake.gotReqs)-1].Model; last != "claude-sonnet-4-5" {
		t.Errorf("judge model = %q, want config judgeModel", last)
	}

	state, err := loadState(context.Background(), store)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state.SinceID != "6" {
		t.Errorf("cursor = %q, want 6 (newest mock tweet id)", state.SinceID)
	}
}

func TestProjectRunSkipsWhenNothingKept(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOMATION_ROOT", root)

	fake := &fakeClient{}
	sink := &fakeSink{}

	var buf bytes.Buffer
	runTime := &runner.Runtime{DryRun: false, Log: log.New(&buf, "", 0), ProjectDir: "."}

	store := &storage.FS{Root: root}
	p := &project{client: fake, source: quietSource{}, sinks: []sinks.Sink{sink}, store: store}

	if err := p.Run(context.Background(), runTime); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if fake.gotReq.Prompt != "" {
		t.Errorf("LLM was called even though nothing was kept")
	}
	if len(sink.delivered) != 0 {
		t.Errorf("delivered %d messages, want 0", len(sink.delivered))
	}

	state, err := loadState(context.Background(), store)
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
		{"name": "sang", "sink": "console", "topics": ["AI"]},
		{"name": "thomas", "sink": "console", "topics": ["*"]}
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
		store:  &storage.FS{Root: root},
		jobs:   queue.NewMemory(),
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

	sang := got["sang"]
	if sang == nil || len(sang.delivered) != 1 ||
		!strings.Contains(sang.delivered[0], "ai story") ||
		strings.Contains(sang.delivered[0], "misc story") {
		t.Errorf("sang (AI only) got wrong digest: %#v", sang)
	}
	thomas := got["thomas"]
	if thomas == nil || len(thomas.delivered) != 1 ||
		!strings.Contains(thomas.delivered[0], "ai story") ||
		!strings.Contains(thomas.delivered[0], "misc story") {
		t.Errorf("thomas (wildcard) got wrong digest: %#v", thomas)
	}
}

type langClient struct {
	prompts    []string
	judgeCalls int
}

func (m *langClient) Complete(ctx context.Context, req ai.Request) (ai.Response, error) {
	if strings.Contains(req.Prompt, "quality evaluator") {
		m.judgeCalls++
		return ai.Response{Text: verdictJSON, Model: "claude-haiku-4-5", Usage: ai.Usage{InputTokens: 5, OutputTokens: 5}}, nil
	}
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
		{"name": "sang", "sink": "console", "topics": ["AI"]},
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
		store:  &storage.FS{Root: root},
		jobs:   queue.NewMemory(),
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
	if fake.judgeCalls != 2 {
		t.Errorf("judge called %d times, want 2 (one per language)", fake.judgeCalls)
	}
	if a := got["sang"]; a == nil || len(a.delivered) != 1 || !strings.Contains(a.delivered[0], "english story") {
		t.Errorf("sang got wrong digest: %#v", a)
	}
	if h := got["hana"]; h == nil || len(h.delivered) != 1 || !strings.Contains(h.delivered[0], "korean story") {
		t.Errorf("hana got wrong digest: %#v", h)
	}
}

func TestFailedSubscriberRetriesNextRunWithoutLoss(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOMATION_ROOT", root)

	dir := filepath.Join(root, "projects", "twitter-digest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	subsJSON := `[
		{"name": "sang", "sink": "console", "topics": ["*"]},
		{"name": "thomas", "sink": "email", "topics": ["*"]}
	]`
	if err := os.WriteFile(filepath.Join(dir, "subscribers.json"), []byte(subsJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := &fakeClient{resp: ai.Response{
		Text:  "## AI\n- big story",
		Model: "claude-haiku-4-5",
		Usage: ai.Usage{InputTokens: 10, OutputTokens: 20},
	}}

	store := &storage.FS{Root: root}
	jobs := queue.NewMemory()

	sang := &fakeSink{}
	thomas := &fakeSink{}
	thomasDown := true

	p := &project{
		client: fake,
		source: sources.Mock{},
		store:  store,
		jobs:   jobs,
		sinkFor: func(sub Subscriber, cfg Config, rt *runner.Runtime) (sinks.Sink, error) {
			if sub.Name == "thomas" {
				if thomasDown {
					return nil, errors.New("email sink misconfigured")
				}
				return thomas, nil
			}
			return sang, nil
		},
	}
	var buf bytes.Buffer
	runTime := &runner.Runtime{DryRun: false, Log: log.New(&buf, "", 0), ProjectDir: "."}

	// run 1: thomas's sink is down
	if err := p.Run(context.Background(), runTime); err != nil {
		t.Fatalf("run 1 failed: %v", err)
	}
	if len(sang.delivered) != 1 {
		t.Errorf("run 1: sang deliveries = %d, want 1", len(sang.delivered))
	}
	if len(thomas.delivered) != 0 {
		t.Errorf("run 1: thomas deliveries = %d, want 0", len(thomas.delivered))
	}
	state, err := loadState(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if state.SinceID != "6" {
		t.Errorf("cursor = %q, want 6: it must advance even when a subscriber fails", state.SinceID)
	}
	// run 2: thomas recovered - his queued job delivers, sang is not repeated
	thomasDown = false
	if err := p.Run(context.Background(), runTime); err != nil {
		t.Fatalf("run 2 failed: %v", err)
	}
	if len(thomas.delivered) != 1 {
		t.Errorf("run 2: thomas deliveries = %d, want 1 (queued job must be retried)", len(thomas.delivered))
	}
	if len(sang.delivered) != 1 {
		t.Errorf("run 2: sang deliveries = %d, want 1 (dedupe must prevent a double-send)", len(sang.delivered))
	}

}
func TestQuietRunStillDrainsPendingJobs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOMATION_ROOT", root)
	dir := filepath.Join(root, "projects", "twitter-digest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	subsJSON := `[{"name": "sang", "sink": "console", "topics": ["*"]}]`
	if err := os.WriteFile(filepath.Join(dir, "subscribers.json"), []byte(subsJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	jobs := queue.NewMemory()
	// yesterday's failed delivery, still pending
	if err := jobs.Enqueue(context.Background(), "twitter-digest", queue.Job{
		ID: "5#sang", Payload: []byte("yesterday's digest"),
	}); err != nil {
		t.Fatal(err)
	}
	sang := &fakeSink{}
	p := &project{
		client: &fakeClient{},
		source: quietSource{}, // fetches one spam tweet, keeps nothing
		store:  &storage.FS{Root: root},
		jobs:   jobs,
		sinkFor: func(sub Subscriber, cfg Config, rt *runner.Runtime) (sinks.Sink, error) {
			return sang, nil
		},
	}
	var buf bytes.Buffer
	runTime := &runner.Runtime{DryRun: false, Log: log.New(&buf, "", 0), ProjectDir: "."}
	if err := p.Run(context.Background(), runTime); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(sang.delivered) != 1 || sang.delivered[0] != "yesterday's digest" {
		t.Errorf("pending job not drained on quiet run: %#v", sang.delivered)
	}
}

func TestDeadLetterAlertsOperator(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOMATION_ROOT", root)
	dir := filepath.Join(root, "projects", "twitter-digest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	subsJSON := `[{"name": "sang", "sink": "console", "topics": ["*"]}]`
	if err := os.WriteFile(filepath.Join(dir, "subscribers.json"), []byte(subsJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	jobs := queue.NewMemory()

	if err := jobs.Enqueue(ctx, "twitter-digest", queue.Job{ID: "5#sang", Payload: []byte("doomed digest")}); err != nil {
		t.Fatal(err)
	}

	// burn attempts 1-4 through the queue API so the run performs the fatal fifth
	for i := 0; i < 4; i++ {
		if ok, _ := jobs.Claim(ctx, "twitter-digest", "5#sang", time.Minute); !ok {
			t.Fatalf("setup claim %d failed", i+1)
		}
		if err := jobs.Fail(ctx, "twitter-digest", "5#sang", errors.New("sink down"), false); err != nil {
			t.Fatal(err)
		}
	}

	alert := &fakeSink{}

	p := &project{
		client: &fakeClient{},
		source: quietSource{},
		store:  &storage.FS{Root: root},
		jobs:   jobs,
		alert:  alert,
		sinkFor: func(sub Subscriber, cfg Config, rt *runner.Runtime) (sinks.Sink, error) {
			return nil, errors.New("sink still down")
		},
	}

	var buf bytes.Buffer
	runTime := &runner.Runtime{DryRun: false, Log: log.New(&buf, "", 0), ProjectDir: "."}
	if err := p.Run(context.Background(), runTime); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(alert.delivered) != 1 || !strings.Contains(alert.delivered[0], "gave up after 5 attempts") {
		t.Errorf("operator alert = %#v, want one dead-letter notice", alert.delivered)
	}
	pending, _ := jobs.Pending(ctx, "twitter-digest")
	if len(pending) != 0 {
		t.Errorf("dead-lettered job still pending: %#v", pending)
	}

}

func readArtifact(t *testing.T, root, lang string) Artifact {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "logs", "runs", "*-"+lang+".json"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("want exactly one %s artifact, got %v (err: %v)", lang, matches, err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var a Artifact
	if err := json.Unmarshal(data, &a); err != nil {
		t.Fatalf("unmarshal artifact: %v", err)
	}
	return a
}

func TestJudgeVerdictsRecordedInArtifact(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOMATION_ROOT", root)
	dir := filepath.Join(root, "projects", "twitter-digest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	subsJSON := `[{"name": "sang", "sink": "console", "topics": ["*"]}]`
	if err := os.WriteFile(filepath.Join(dir, "subscribers.json"), []byte(subsJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	sink := &fakeSink{}
	p := &project{
		client: &langClient{}, // answers judge prompts with verdictJSON
		source: sources.Mock{},
		store:  &storage.FS{Root: root},
		jobs:   queue.NewMemory(),
		sinkFor: func(sub Subscriber, cfg Config, rt *runner.Runtime) (sinks.Sink, error) {
			return sink, nil
		},
	}
	var buf bytes.Buffer
	runTime := &runner.Runtime{DryRun: false, Log: log.New(&buf, "", 0), ProjectDir: "."}
	if err := p.Run(context.Background(), runTime); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	a := readArtifact(t, root, "english")
	if a.JudgeError != "" {
		t.Errorf("unexpected judge error: %q", a.JudgeError)
	}
	if a.Judge == nil {
		t.Fatal("judge verdicts missing from artifact")
	}
	if !a.Judge.Faithfulness.Pass || a.Judge.Coverage.Pass {
		t.Errorf("verdicts not preserved: %+v", a.Judge)
	}
	if a.Judge.Coverage.Reason != "dropped the Dario tweet" {
		t.Errorf("failure reason not preserved: %q", a.Judge.Coverage.Reason)
	}
}

func TestJudgeFailureNeverBlocksDelivery(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOMATION_ROOT", root)
	dir := filepath.Join(root, "projects", "twitter-digest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	subsJSON := `[{"name": "sang", "sink": "console", "topics": ["*"]}]`
	if err := os.WriteFile(filepath.Join(dir, "subscribers.json"), []byte(subsJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	sink := &fakeSink{}
	p := &project{
		// answers every prompt with digest text: the judge gets unparseable output
		client: &fakeClient{resp: ai.Response{Text: "## AI\n- big story", Model: "claude-haiku-4-5"}},
		source: sources.Mock{},
		store:  &storage.FS{Root: root},
		jobs:   queue.NewMemory(),
		sinkFor: func(sub Subscriber, cfg Config, rt *runner.Runtime) (sinks.Sink, error) {
			return sink, nil
		},
	}
	var buf bytes.Buffer
	runTime := &runner.Runtime{DryRun: false, Log: log.New(&buf, "", 0), ProjectDir: "."}
	if err := p.Run(context.Background(), runTime); err != nil {
		t.Fatalf("a judge failure must never fail the run: %v", err)
	}
	if len(sink.delivered) != 1 {
		t.Errorf("deliveries = %d, want 1: delivery must not depend on the judge", len(sink.delivered))
	}

	a := readArtifact(t, root, "english")
	if a.Judge != nil || a.JudgeError == "" {
		t.Errorf("want nil verdicts and a recorded judge error, got %+v / %q", a.Judge, a.JudgeError)
	}
}
