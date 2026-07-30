package twitterdigest

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/internal/queue"
	"github.com/sl6117/automations/internal/runner"
	"github.com/sl6117/automations/internal/storage"
	"github.com/sl6117/automations/pkg/sinks"
	"github.com/sl6117/automations/pkg/sources"
)

// truncatingClient answers the judge cleanly and returns a digest that either did or did
// not hit the token ceiling, so the two cases differ ONLY by the stop reason.
type truncatingClient struct {
	truncate bool
}

func (m *truncatingClient) Complete(ctx context.Context, req ai.Request) (ai.Response, error) {
	if strings.Contains(req.Prompt, "quality evaluator") {
		return ai.Response{Text: passAllJSON, Usage: ai.Usage{InputTokens: 5, OutputTokens: 5}}, nil
	}
	resp := ai.Response{
		Text:  "## AI\n- a story that stops mid-sen",
		Usage: ai.Usage{InputTokens: 10, OutputTokens: 1500},
	}
	if m.truncate {
		resp.StopReason = ai.StopMaxTokens
	}
	return resp, nil
}

// runForTruncation drives one full run and returns the English artifact.
//
// Korean and Russian digests silently hit the 1500-token ceiling every day: the model was
// cut off mid-sentence and whole trailing sections were never generated. Because the
// `## <English topic>` headers route sections to subscribers, a severed tail means those
// subscribers receive nothing at all, with no error anywhere. Only the judge ever noticed,
// inconsistently, and it has no obligation to.
func runForTruncation(t *testing.T, truncate bool) Artifact {
	t.Helper()
	root := t.TempDir()
	t.Setenv("AUTOMATION_ROOT", root)
	dir := filepath.Join(root, "projects", "twitter-digest")
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"config.json":       `{"topics":[{"name":"AI"}],"source":"mock","model":"m"}`,
		"subscribers.json":  `[{"name":"sang","sink":"console","topics":["AI"]}]`,
		"prompts/digest.md": "write a digest in {{LANGUAGE}}\n{{TOPICS}}\n{{TWEETS_JSON}}",
		"prompts/judge.md":  "quality evaluator {{LANGUAGE}}\n{{TOPICS}}\n{{TWEETS_JSON}}\n{{DIGEST}}",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := &project{
		client: &truncatingClient{truncate: truncate},
		source: sources.Mock{},
		store:  &storage.FS{Root: root},
		jobs:   queue.NewMemory(),
		sinkFor: func(sub Subscriber, cfg Config, rt *runner.Runtime) (sinks.Sink, error) {
			return &fakeSink{}, nil
		},
	}
	runTime := &runner.Runtime{DryRun: false, Log: log.New(io.Discard, "", 0), ProjectDir: dir}
	if err := p.Run(context.Background(), runTime); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	return readArtifact(t, root, "english")
}

// A digest cut off at the ceiling has to be recorded as such. Without this the archive
// cannot tell a short news day from a severed digest, and the truncation stays invisible.
func TestArtifactRecordsTruncatedDigest(t *testing.T) {
	a := runForTruncation(t, true)

	if !a.Truncated {
		t.Error("truncated = false, but the model stopped at the token ceiling")
	}
}

// The ordinary case must stay clean, or the flag means nothing.
func TestArtifactDoesNotFlagCompleteDigest(t *testing.T) {
	a := runForTruncation(t, false)

	if a.Truncated {
		t.Error("truncated = true, but the model finished on its own")
	}
}
