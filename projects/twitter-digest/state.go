package twitterdigest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/sl6117/automations/pkg/sources"
)

// State holds run cursors persisted between runs. Anchored on AUTOMATION_ROOT
// (like obs.LogRun) so tests with a temp root never touch the real file.
type State struct {
	SinceID string `json:"sinceId"`
}

func statePath() string {
	root := os.Getenv("AUTOMATION_ROOT")
	if root == "" {
		root = "."
	}
	return filepath.Join(root, "projects", "twitter-digest", "state.json")
}

// loadState returns a zero State on first run (no file yet)
func loadState() (State, error) {
	data, err := os.ReadFile(statePath())
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read state: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("parse state: %w", err)
	}
	return state, nil
}

func saveState(state State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(statePath()), 0o755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	if err := os.WriteFile(statePath(), data, 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

// newestID returns the numerically largest tweet ID or "" if none parse
// Snowflake IDs grow over time, so largest = most recent
func newestID(tweets []sources.Tweet) string {
	var max uint64
	out := ""
	for _, tweet := range tweets {
		id, err := strconv.ParseUint(tweet.ID, 10, 64)
		if err != nil {
			continue
		}
		if id > max {
			max, out = id, tweet.ID
		}
	}
	return out
}
