// package obs record what each automation run cost: it appends one JSON line
// per run to logs/cost-log.jsonl. It's generic and knows nothing about any specific project
package obs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Prices in USD per 1,000,000 tokens (input, output). Model keys are lowercase.
var Prices = map[string]struct{ In, Out float64 }{
	"anthropic/claude-haiku-4.5":  {In: 1.0, Out: 5.0},
	"anthropic/claude-sonnet-4.6": {In: 3.0, Out: 15.0},
	"anthropic/claude-opus-4.6":   {In: 5.0, Out: 25.0},

	// anthropic direct
	"claude-haiku-4-5": {In: 1.0, Out: 5.0},
}

// EstimateCost returns the USD cost for a model's token usage, or 0 if the model isn't in the price table.
func EstimateCost(model string, inputTokens, outputTokens int) float64 {
	p, ok := Prices[strings.ToLower(model)]

	if !ok {
		return 0
	}
	return float64(inputTokens)/1e6*p.In + float64(outputTokens)/1e6*p.Out
}

// Run is one logged automation run. Timestamp and CostUSD are filled by LogRun
type Run struct {
	Timestamp    string  `json:"ts"`
	Project      string  `json:"project"`
	Model        string  `json:"model"`
	DryRun       bool    `json:"dryRun"`
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	CostUSD      float64 `json:"costUsd"`
	ItemCount    int     `json:"itemCount"`
}

func logRoot() string {
	if root := os.Getenv("AUTOMATION_ROOT"); root != "" {
		return root
	}
	return "."
}

// LogRun appends run as one JSON line to logs/cost-log.jsonl under
// AUTOMATION_ROOT (fallback to current directory)
func LogRun(run Run) (Run, error) {
	run.Timestamp = time.Now().UTC().Format(time.RFC3339)

	// dry run -> no cost
	if run.DryRun {
		run.CostUSD = 0
	} else {
		run.CostUSD = EstimateCost(run.Model, run.InputTokens, run.OutputTokens)
	}

	// build "<AUTOMATION_ROOT>/logs" (or "./logs" if env var isn't set)
	dir := filepath.Join(logRoot(), "logs")

	// create the directory (and any missing parent directories).
	// MkdirAll is a no-op if it already exists, so it's safe to call every run
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Run{}, fmt.Errorf("create log dir: %w", err)
	}

	// turn the Run struct into a JSON object (a []byte), using the json tags
	line, err := json.Marshal(run)
	if err != nil {
		return Run{}, fmt.Errorf("marshal run: %w", err)
	}

	// Open the log file in append/create/write-only mode (explained above)
	f, err := os.OpenFile(filepath.Join(dir, "cost-log.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return Run{}, fmt.Errorf("open cost log file: %w", err)
	}

	// Guarantee the file handle is closed when the funciton returns,
	// no matter which path we exit through
	defer f.Close()

	// Write the JSON followed by a newline. ".jsonl" = "JSON Lines":
	// one complete JSON object per line. append(line, '\n') tacks the newline byte onto end of JSON byte slice.
	if _, err := f.Write(append(line, '\n')); err != nil {
		return Run{}, fmt.Errorf("write to cost log file: %w", err)
	}

	// Return the enriched record (with timestamp and cost)
	// caller can log/inspect it if it wants
	return run, nil
}
