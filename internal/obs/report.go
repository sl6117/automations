package obs

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// costLogPath is the same file LogRun writes to
func costLogPath() string {
	return filepath.Join(logRoot(), "logs", "cost-log.jsonl")
}

// LoadRuns reads every logged run. A missing log isn't an error (returns nil)
func LoadRuns() ([]Run, error) {
	f, err := os.Open(costLogPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open cost log: %w", err)
	}
	defer f.Close()

	var runs []Run
	scanner := bufio.NewScanner(f)
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
	cost   float64
}

// Report aggregates the cost log and writes a human-readable summary to w
func Report(w io.Writer) error {
	runs, err := LoadRuns()
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		fmt.Fprintf(w, "No cost log yet at %s\n", costLogPath())
		return nil
	}

	byMonth := map[string]*monthAgg{}
	var totalCost float64
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
		agg.cost += r.CostUSD
		totalCost += r.CostUSD
		totalTokens += tokens

		if !r.DryRun {
			rr := r
			lastReal = &rr
		}
	}

	fmt.Fprintln(w, "LLM Cost Report")
	fmt.Fprintln(w, "================")
	fmt.Fprintf(w, "Runs: %d   Tokens: %d   Cost: $%.4f\n\n", len(runs), totalTokens, totalCost)
	fmt.Fprintln(w, "By month:")

	months := make([]string, 0, len(byMonth))
	for month := range byMonth {
		months = append(months, month)
	}
	sort.Strings(months)
	for _, m := range months {
		agg := byMonth[m]
		fmt.Fprintf(w, "  %s: %d runs, %d tokens, $%.4f\n", m, agg.runs, agg.tokens, agg.cost)
	}

	if lastReal != nil {
		fmt.Fprintf(w, "\nLast real run: %s (%s) $%.6f for %d items\n",
			lastReal.Timestamp, lastReal.Model, lastReal.CostUSD, lastReal.ItemCount)
	}
	return nil

}
