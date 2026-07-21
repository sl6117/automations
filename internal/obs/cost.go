// package obs record what each automation run cost: it appends one JSON line
// per run to logs/cost-log.jsonl. It's generic and knows nothing about any specific project
package obs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sl6117/automations/internal/storage"
)

// Prices in USD per 1,000,000 tokens (input, output). Model keys are lowercase.
var Prices = map[string]struct{ In, Out float64 }{
	"anthropic/claude-haiku-4.5":  {In: 1.0, Out: 5.0},
	"anthropic/claude-sonnet-4.6": {In: 3.0, Out: 15.0},
	"anthropic/claude-opus-4.6":   {In: 5.0, Out: 25.0},

	// anthropic direct
	"claude-haiku-4-5": {In: 1.0, Out: 5.0},
}

// the storage key of the append-only run log; exported so
// read-side consumers (auto cost, digest-mcp) share one definition.
const CostLogKey = "logs/cost-log.jsonl"

// EstimateCost returns the USD cost for a model's token usage, or 0 if the model isn't in the price table.
func EstimateCost(model string, inputTokens, outputTokens int) float64 {
	p, ok := Prices[strings.ToLower(model)]

	if !ok {
		return 0
	}
	return float64(inputTokens)/1e6*p.In + float64(outputTokens)/1e6*p.Out
}

// Run is one logged automation run. Timestamp and CostUSD are filled by LogRun
// SourceReads counts billed X API reads (pages x 50); unlike CostUSD it is real
// spend even on dry runs, so it is never zeroed.
type Run struct {
	Timestamp    string  `json:"ts"`
	Project      string  `json:"project"`
	Model        string  `json:"model"`
	DryRun       bool    `json:"dryRun"`
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	CostUSD      float64 `json:"costUsd"`
	ItemCount    int     `json:"itemCount"`
	SourceReads  int     `json:"sourceReads,omitempty"`
}

func logRoot() string {
	if root := os.Getenv("AUTOMATION_ROOT"); root != "" {
		return root
	}
	return "."
}

// LogRun appends run as one JSON line to logs/cost-log.jsonl under
// AUTOMATION_ROOT (fallback to current directory)
func LogRun(ctx context.Context, store storage.Store, run Run) (Run, error) {
	run.Timestamp = time.Now().UTC().Format(time.RFC3339)

	// dry run -> no cost
	if run.DryRun {
		run.CostUSD = 0
	} else {
		run.CostUSD = EstimateCost(run.Model, run.InputTokens, run.OutputTokens)
	}

	line, err := json.Marshal(run)
	if err != nil {
		return Run{}, fmt.Errorf("marshal run: %w", err)
	}

	if err := store.Append(ctx, CostLogKey, line); err != nil {
		return Run{}, fmt.Errorf("append to cost log: %w", err)
	}
	return run, nil
}
