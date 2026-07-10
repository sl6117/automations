package twitterdigest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sl6117/automations/internal/storage"
	"github.com/sl6117/automations/pkg/sources"
)

// Artifact preserves eerything needed to re-inspect (or later re-judge)
// one run: the exact inputs the model saw and the exact output it gave
type Artifact struct {
	Timestamp    string          `json:"ts"`
	Model        string          `json:"model"`
	Kept         []sources.Tweet `json:"kept"`
	Language     string          `json:"language"`
	Digest       string          `json:"digest"`
	InputTokens  int             `json:"inputTokens"`
	OutputTokens int             `json:"outputTokens"`
	EvalFailures []string        `json:"evalFailures"`
	EvalCoverage string          `json:"evalCoverage"`
	Judge        *JudgeReport    `json:"judge,omitempty"`
	JudgeError   string          `json:"judgeError,omitempty"`
}

// saveArtifact writes one artifact per run under logs/runs/ via the
// storage.Storage, perserving the exact inpiuts and output for re-inspection.
func saveArtifact(ctx context.Context, store storage.Store, a Artifact) error {
	now := time.Now().UTC()
	a.Timestamp = now.Format(time.RFC3339)

	data, err := json.MarshalIndent(a, "", " ")
	if err != nil {
		return fmt.Errorf("marshal artifact: %w", err)
	}

	key := "logs/runs/" + now.Format("2006-01-02T15-04-05Z") + "-twitter-digest-" + strings.ToLower(strings.ReplaceAll(a.Language, " ", "-")) + ".json"

	if err := store.Put(ctx, key, data); err != nil {
		return fmt.Errorf("write artifact: %w", err)
	}
	return nil
}
