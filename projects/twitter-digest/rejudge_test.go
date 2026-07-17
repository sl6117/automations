package twitterdigest

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/storage"
	"github.com/sl6117/automations/pkg/sources"
)

func putArtifact(t *testing.T, store storage.Store, key string, a Artifact) {
	t.Helper()
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), key, data); err != nil {
		t.Fatal(err)
	}
}

func TestReplayJudgeSkipsAlreadyJudgedUnlessForced(t *testing.T) {
	ctx := context.Background()
	store := &storage.FS{Root: t.TempDir()}
	kept := []sources.Tweet{{Author: "Dario", Handle: "@d", Text: "AI", URL: "https://x.com/i/1"}}
	putArtifact(t, store, "logs/runs/2026-07-07T00-00-00Z-twitter-digest-english.json", Artifact{
		Timestamp: "2026-07-07T00:00:00Z", Language: "English", Digest: "## AI\n- story", Kept: kept,
	})
	putArtifact(t, store, "logs/runs/2026-07-08T00-00-00Z-twitter-digest-english.json", Artifact{
		Timestamp: "2026-07-08T00:00:00Z", Language: "English", Digest: "## AI\n- story", Kept: kept,
		Judge: &JudgeReport{},
	})
	putArtifact(t, store, "logs/runs/2026-07-09T00-00-00Z-twitter-digest-english.json", Artifact{
		Timestamp: "2026-07-09T00:00:00Z", Language: "English", Digest: "## AI\n- story", Kept: kept,
		JudgeError: "judge timed out",
	})
	fake := &langClient{}
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	if err := ReplayJudge(ctx, store, fake, ".", false, logger); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if fake.judgeCalls != 2 {
		t.Errorf("judge calls = %d, want 2 (only the unjudged artifact)", fake.judgeCalls)
	}
	if !strings.Contains(buf.String(), "2 judged, 1 skipped") {
		t.Errorf("summary missing from log: %s", buf.String())
	}
	data, err := store.Get(ctx, "logs/runs/2026-07-07T00-00-00Z-twitter-digest-english.json")
	if err != nil {
		t.Fatal(err)
	}
	var a Artifact
	if err := json.Unmarshal(data, &a); err != nil {
		t.Fatal(err)
	}
	if a.Judge == nil || a.Judge.Coverage.Pass {
		t.Errorf("verdicts not written back to the artifact: %+v", a.Judge)
	}
	if a.Timestamp != "2026-07-07T00:00:00Z" {
		t.Errorf("timestamp was rewritten: %q", a.Timestamp)
	}
	if err := ReplayJudge(ctx, store, fake, ".", true, logger); err != nil {
		t.Fatalf("replay force: %v", err)
	}
	if fake.judgeCalls != 5 {
		t.Errorf("judge calls = %d, want 5 (2 from before + all 3 forced)", fake.judgeCalls)
	}
}
