package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/storage"
	"github.com/sl6117/automations/pkg/sources"
	twitterdigest "github.com/sl6117/automations/projects/twitter-digest"
)

func seedArtifact(t *testing.T, store storage.Store, key string, a twitterdigest.Artifact) {
	t.Helper()
	data, err := json.Marshal(a)

	if err != nil {
		t.Fatal(err)
	}

	if err := store.Put(context.Background(), key, data); err != nil {
		t.Fatal(err)
	}
}

func TestGetArtifactOmitsTweetsByDefault(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}

	s := &digestServer{store: store}
	key := "logs/runs/2026-07-21T16-00-26Z-twitter-digest-english.json"

	seedArtifact(t, store, key, twitterdigest.Artifact{
		Language: "English",
		Digest:   "## AI\n- story",
		Kept:     []sources.Tweet{{Author: "Dario", Handle: "@d", Text: "AI", URL: "https://x.com/i/1"}},
	})

	_, out, err := s.getArtifact(context.Background(), nil, getArtifactInput{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	if out.Artifact.Kept != nil {
		t.Errorf("Kept = %v, want nil without includeTweets", out.Artifact.Kept)
	}
	if !strings.Contains(out.Artifact.Digest, "## AI") {
		t.Errorf("Digest = %q, want the seeded digest", out.Artifact.Digest)
	}

	_, out, err = s.getArtifact(context.Background(), nil, getArtifactInput{Key: key, IncludeTweets: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Artifact.Kept) != 1 {
		t.Errorf("Kept length = %d, want 1 with includeTweets", len(out.Artifact.Kept))
	}
}
