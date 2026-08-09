package weeklydeepdive

import (
	"context"
	"sync"

	"github.com/sl6117/automations/internal/ai"
)

type researchResult struct {
	Report    ResearchReport
	Usage     ai.Usage
	Truncated bool
}

// researchFn runs one question. index matches the questions slice.
type researchFn func(ctx context.Context, index int, question string) (researchResult, error)

// researchFanOut runs all questions concurrently, fail-fast on first error,
// and returns reports in question order.
func researchFanOut(ctx context.Context, questions []string, run researchFn) ([]ResearchReport, ai.Usage, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	reports := make([]ResearchReport, len(questions))
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		usage    ai.Usage
	)

	for i, q := range questions {
		wg.Add(1)
		go func(i int, q string) {
			defer wg.Done()
			res, err := run(ctx, i, q)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				return
			}
			reports[i] = res.Report
			usage.InputTokens += res.Usage.InputTokens
			usage.OutputTokens += res.Usage.OutputTokens
			usage.CacheCreationInputTokens += res.Usage.CacheCreationInputTokens
			usage.CacheReadInputTokens += res.Usage.CacheReadInputTokens
		}(i, q)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, usage, firstErr
	}
	return reports, usage, nil
}
