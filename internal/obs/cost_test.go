package obs

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/storage"
)

func TestEstimateCost(t *testing.T) {
	// haiku: $1/MTok in, $5/MTok out — no cache
	if got := EstimateCost("anthropic/claude-haiku-4.5", 1_000_000, 1_000_000, 0, 0); got != 6.0 {
		t.Errorf("cost = %v, want 6.0", got)
	}
	if got := EstimateCost("unknown/model", 1000, 1000, 0, 0); got != 0 {
		t.Errorf("unknown model cost = %v, want 0", got)
	}
}

// 5-minute ephemeral cache: writes 1.25x base input, reads 0.1x. Output unchanged.
func TestEstimateCostPricesCacheTokens(t *testing.T) {
	// 1M uncached in ($1) + 1M cache write ($1.25) + 1M cache read ($0.10) + 0 out
	got := EstimateCost("claude-haiku-4-5", 1_000_000, 0, 1_000_000, 1_000_000)
	want := 1.0 + 1.25 + 0.10
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("cost = %v, want %v", got, want)
	}
	// zero cache args must match the pre-caching formula
	if got := EstimateCost("claude-haiku-4-5", 1_000_000, 1_000_000, 0, 0); got != 6.0 {
		t.Errorf("no-cache cost = %v, want 6.0", got)
	}
}

func TestLogRunPricesCacheFields(t *testing.T) {
	ctx := context.Background()
	store := &storage.FS{Root: t.TempDir()}

	rec, err := LogRun(ctx, store, Run{
		Project:                  "weekly-deepdive",
		Model:                    "claude-haiku-4-5",
		InputTokens:              100_000, // uncached tail
		CacheCreationInputTokens: 400_000,
		CacheReadInputTokens:     500_000,
		OutputTokens:             0,
	})
	if err != nil {
		t.Fatalf("LogRun: %v", err)
	}
	// 0.1 + 0.4*1.25 + 0.5*0.1 = 0.1 + 0.5 + 0.05 = 0.65
	// float64 multiply/divide can land one ULP off exact 0.65
	if math.Abs(rec.CostUSD-0.65) > 1e-9 {
		t.Errorf("CostUSD = %v, want 0.65", rec.CostUSD)
	}
	if rec.CacheCreationInputTokens != 400_000 || rec.CacheReadInputTokens != 500_000 {
		t.Errorf("cache fields not persisted: %+v", rec)
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
		SourceReads: 150,
	}); err != nil {
		t.Fatalf("LogRun failed: %v", err)
	}

	data, err := store.Get(ctx, CostLogKey)
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

	if rec.SourceReads != 150 {
		t.Errorf("sourceReads = %d, want 150", rec.SourceReads)
	}
}

func TestLogRunDryRunKeepsSourceReads(t *testing.T) {
	ctx := context.Background()
	store := &storage.FS{Root: t.TempDir()}

	rec, err := LogRun(ctx, store, Run{
		Project:     "twitter-digest",
		Model:       "claude-haiku-4-5",
		DryRun:      true,
		InputTokens: 1_000_000,
		SourceReads: 100,
	})
	if err != nil {
		t.Fatalf("LogRun failed: %v", err)
	}
	// dry run zeroes LLM cost (no model was called) but X reads are real spend
	if rec.CostUSD != 0 {
		t.Errorf("dry-run cost = %v, want 0", rec.CostUSD)
	}
	if rec.SourceReads != 100 {
		t.Errorf("dry-run sourceReads = %d, want 100", rec.SourceReads)
	}
}
