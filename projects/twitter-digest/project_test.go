package twitterdigest

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/internal/runner"
)

func TestProjectRun(t *testing.T) {
	t.Setenv("AUTOMATION_ROOT", t.TempDir())
	var buf bytes.Buffer
	runTime := &runner.Runtime{
		DryRun:     true,
		Log:        log.New(&buf, "", 0),
		ProjectDir: ".",
	}

	if err := (&project{}).Run(context.Background(), runTime); err != nil {
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

	var buf bytes.Buffer
	runTime := &runner.Runtime{DryRun: false, Log: log.New(&buf, "", 0), ProjectDir: "."}

	if err := (&project{client: fake}).Run(context.Background(), runTime); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "Dario says AI is coming") {
		t.Errorf("output missing model text\n---\n%s", got)
	}
	if !strings.Contains(fake.gotReq.Prompt, "Dario") {
		t.Errorf("prompt to model missing a kept tweet:\n%s", fake.gotReq.Prompt)
	}
	if strings.Contains(fake.gotReq.Prompt, "@cryptomoonboy") {
		t.Errorf("spam reached the model prompt (filter bypassed):\n%s", fake.gotReq.Prompt)
	}
	if fake.gotReq.Model != "claude-haiku-4-5" {
		t.Errorf("model = %q, want config model", fake.gotReq.Model)
	}
}
