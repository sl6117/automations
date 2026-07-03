package twitterdigest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sl6117/automations/pkg/sources"
)

// Artifact preserves eerything needed to re-inspect (or later re-judge)
// one run: the exact inputs the model saw and the exact output it gave
type Artifact struct {
	Timestamp    string          `json:"ts"`
	Model        string          `json:"model"`
	Kept         []sources.Tweet `json:"kept"`
	Digest       string          `json:"digest"`
	InputTokens  int             `json:"inputTokens"`
	OutputTokens int             `json:"outputTokens"`
	EvalFailures []string        `json:"evalFailures"`
	EvalCoverage string          `json:"evalCoverage"`
}

// saveArtifact writes one JSON file per run under logs/runs/
// (AUTOMATION_ROOT-anchored, same pattern as the cost log and state file).
func saveArtifact(a Artifact) error {
	now := time.Now().UTC()
	a.Timestamp = now.Format(time.RFC3339)

	root := os.Getenv("AUTOMATION_ROOT")
	if root == "" {
		root = "."
	}
	dir := filepath.Join(root, "logs", "runs")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create runs dir: %w", err)
	}

	data, err := json.MarshalIndent(a, "", " ")
	if err != nil {
		return fmt.Errorf("marshal artifact: %w", err)
	}

	name := now.Format("2006-01-02T15-04-05Z") + "-twitter-digest.json"
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return fmt.Errorf("write artifact: %w", err)
	}
	return nil
}
