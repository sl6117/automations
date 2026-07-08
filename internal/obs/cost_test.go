package obs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/storage"
)

func TestEstimateCost(t *testing.T) {
	if got := EstimateCost("anthropic/claude-haiku-4.5", 1_000_000, 1_000_000); got != 6.0 {
		t.Errorf("cost = %v, want 6.0", got)
	}
	if got := EstimateCost("unknown/model", 1000, 1000); got != 0 {
		t.Errorf("unknown model cost = %v, want 0", got)
	}
}

func TestLogRunAppends(t *testing.T) {
	ctx := context.Background()
	store := &storage.FS{Root: t.TempDir()}

	if _, err := LogRun(ctx, store, Run{
		Project:     "twitter-digest",
		Model:       "anthropic/claude-haiku-4.5",
		InputTokens: 1_000_000,
		ItemCount:   4,
	}); err != nil {
		t.Fatalf("LogRun failed: %v", err)
	}

	data, err := store.Get(ctx, costLogKey)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}

	var rec Run
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("parse line: %v", err)
	}

	if rec.Timestamp == "" {
		t.Errorf("timestamp missing")
	}
	if rec.CostUSD != 1.0 {
		t.Errorf("cost = %v, want 1.0", rec.CostUSD)
	}
}
