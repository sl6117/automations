package twitterdigest

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/queue"
	"github.com/sl6117/automations/internal/runner"
	"github.com/sl6117/automations/internal/storage"
	"github.com/sl6117/automations/pkg/sinks"
	"github.com/sl6117/automations/pkg/sources"
)

// runForFunnel drives one full run with a scripted judge and returns the English artifact.
// The verdicts drive the whole loop: the first is the verdict on the first draft, and a
// second (when present) is the re-judge after one revision. reviseBudget is 1.
//
// This exists to prove the artifact captures the revise loop's own funnel. Before this
// bite the artifact stored only the FINAL verdict, so a run that failed faithfulness and
// then got fixed was indistinguishable from a first-try pass — which made the loop the one
// part of the system we could not measure.
func runForFunnel(t *testing.T, verdicts []string) Artifact {
	t.Helper()
	root := t.TempDir()
	t.Setenv("AUTOMATION_ROOT", root)
	dir := filepath.Join(root, "projects", "twitter-digest")
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"config.json":       `{"topics":[{"name":"AI"}],"source":"mock","model":"m","reviseBudget":1}`,
		"subscribers.json":  `[{"name":"sang","sink":"console","topics":["AI"]}]`,
		"prompts/digest.md": "write a digest in {{LANGUAGE}}\n{{TOPICS}}\n{{TWEETS_JSON}}",
		"prompts/judge.md":  "quality evaluator {{LANGUAGE}}\n{{TOPICS}}\n{{TWEETS_JSON}}\n{{DIGEST}}",
		"prompts/revise.md": "You are revising {{LANGUAGE}}\n{{TOPICS}}\n{{TWEETS_JSON}}\n{{DIGEST}}\n{{CRITIQUE}}",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := &project{
		client: &loopClient{verdicts: verdicts},
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

// Funnel stage 0: a clean first draft. The initial verdict must still be recorded, so the
// denominator "how many runs did the judge pass on the first try" is countable.
func TestArtifactRecordsCleanFirstDraft(t *testing.T) {
	a := runForFunnel(t, []string{passAllJSON})

	if a.InitialJudge == nil {
		t.Fatal("initial verdict missing: even a clean first draft needs its verdict recorded")
	}
	if !a.InitialJudge.Faithfulness.Pass {
		t.Errorf("initial faithfulness = fail, want pass: %+v", a.InitialJudge)
	}
	if a.RevisionAdopted {
		t.Error("revisionAdopted = true, but no revision ran")
	}
	if a.Judge == nil || !a.Judge.Faithfulness.Pass {
		t.Errorf("shipped verdict = %+v, want a faithful pass", a.Judge)
	}
}

// Funnel stage 2, the load-bearing case for adoption rate: faithfulness failed, one
// revision cleaned it, the revision shipped. The initial FAILURE and the fact of adoption
// must both survive onto the artifact, because the shipped verdict alone (a pass) looks
// identical to a first-try pass.
func TestArtifactRecordsAdoptedRevision(t *testing.T) {
	a := runForFunnel(t, []string{failFaithJSON, passAllJSON})

	if a.InitialJudge == nil || a.InitialJudge.Faithfulness.Pass {
		t.Fatalf("initial verdict must record the faithfulness FAILURE that triggered revision: %+v", a.InitialJudge)
	}
	if a.InitialJudge.Faithfulness.Reason != "wrong date" {
		t.Errorf("initial failure reason lost: %q", a.InitialJudge.Faithfulness.Reason)
	}
	if !a.RevisionAdopted {
		t.Error("revisionAdopted = false, but a clean revision replaced the draft")
	}
	if a.Judge == nil || !a.Judge.Faithfulness.Pass {
		t.Errorf("shipped verdict = %+v, want the clean re-judge", a.Judge)
	}
	if !strings.Contains(a.Digest, "revised story") {
		t.Errorf("shipped digest is not the revision: %q", a.Digest)
	}
}

// Funnel stage 1-without-2: faithfulness failed, the revision also failed, so the original
// shipped. This is exactly the run the old schema mislabelled as a pass — the initial
// verdict was overwritten only on adoption, so here the shipped verdict is the failing one
// but nothing recorded that a revision was even attempted.
func TestArtifactRecordsFiredButUnadoptedRevision(t *testing.T) {
	a := runForFunnel(t, []string{failFaithJSON, failFaithJSON})

	if a.InitialJudge == nil || a.InitialJudge.Faithfulness.Pass {
		t.Fatalf("initial verdict must record the failure: %+v", a.InitialJudge)
	}
	if a.RevisionAdopted {
		t.Error("revisionAdopted = true, but the revision failed and the original shipped")
	}
	if a.Judge == nil || a.Judge.Faithfulness.Pass {
		t.Errorf("shipped verdict = %+v, want the original failing report", a.Judge)
	}
	if !strings.Contains(a.Digest, "original story") {
		t.Errorf("the original draft must ship when no revision cleans it: %q", a.Digest)
	}
}
