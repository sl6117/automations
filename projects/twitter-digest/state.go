package twitterdigest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/sl6117/automations/internal/storage"
	"github.com/sl6117/automations/pkg/sources"
)

// State holds run cursors persisted between runs (stored under stateKey)
// via storage.Store, so backend and tests decide where it actually lives
type State struct {
	SinceID string `json:"sinceId"`
}

const stateKey = "projects/twitter-digest/state.json"

// loadState returns a zero State on first run (no file yet)
func loadState(ctx context.Context, store storage.Store) (State, error) {

	data, err := store.Get(ctx, stateKey)
	if errors.Is(err, storage.ErrNotExist) {
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

func saveState(ctx context.Context, store storage.Store, state State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := store.Put(ctx, stateKey, data); err != nil {
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
