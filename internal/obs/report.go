package obs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"

	"github.com/sl6117/automations/internal/storage"
)

// costLogPath is the same file LogRun writes to
func costLogPath() string {
	return filepath.Join(logRoot(), "logs", "cost-log.jsonl")
}

// LoadRuns reads every logged run. A missing log isn't an error (returns nil)
func LoadRuns(ctx context.Context, store storage.Store) ([]Run, error) {
	data, err := store.Get(ctx, costLogKey)
	if errors.Is(err, storage.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read cost log: %w", err)
	}

	var runs []Run
	scanner := bufio.NewScanner(bytes.NewReader(data))

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var r Run
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("unmarshal cost log: %w", err)
		}
		runs = append(runs, r)
	}
	return runs, nil
}

type monthAgg struct {
	runs   int
	tokens int
	reads  int
	cost   float64
}

// Report aggregates the cost log and writes a human-readable summary to w
func Report(ctx context.Context, store storage.Store, w io.Writer) error {
	runs, err := LoadRuns(ctx, store)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		fmt.Fprintf(w, "No cost log yet at %s\n", costLogPath())
		return nil
	}

	byMonth := map[string]*monthAgg{}
	var totalCost float64
	var totalReads int
	var totalTokens int
	var lastReal *Run

	for i := range runs {
		r := runs[i]
		tokens := r.InputTokens + r.OutputTokens
		month := r.Timestamp

		if len(month) >= 7 {
			month = month[:7]
		}
		agg := byMonth[month]
		if agg == nil {
			agg = &monthAgg{}
			byMonth[month] = agg
		}
		agg.runs++
		agg.tokens += tokens
		agg.reads += r.SourceReads
		agg.cost += r.CostUSD
		totalCost += r.CostUSD
		totalTokens += tokens
		totalReads += r.SourceReads

		if !r.DryRun {
			rr := r
			lastReal = &rr
		}
	}

	fmt.Fprintln(w, "LLM Cost Report")
	fmt.Fprintln(w, "================")
	fmt.Fprintf(w, "Runs: %d   Tokens: %d   X reads: %d   Cost: $%.4f\n\n", len(runs), totalTokens, totalReads, totalCost)
	fmt.Fprintln(w, "By month:")

	months := make([]string, 0, len(byMonth))
	for month := range byMonth {
		months = append(months, month)
	}
	sort.Strings(months)
	for _, m := range months {
		agg := byMonth[m]
		fmt.Fprintf(w, "  %s: %d runs, %d tokens, %d reads, $%.4f\n", m, agg.runs, agg.tokens, agg.reads, agg.cost)
	}

	if lastReal != nil {
		fmt.Fprintf(w, "\nLast real run: %s (%s) $%.6f for %d items\n",
			lastReal.Timestamp, lastReal.Model, lastReal.CostUSD, lastReal.ItemCount)
	}
	return nil

}
