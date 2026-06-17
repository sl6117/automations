package twitterdigest

import (
	"context"
	"fmt"

	"github.com/sl6117/automations/internal/runner"
	"github.com/sl6117/automations/pkg/sinks"
	"github.com/sl6117/automations/pkg/sources"
)

func init() {
	runner.Register(&project{})
}

type project struct{}

func (p *project) Name() string { return "twitter-digest" }

func (p *project) Run(ctx context.Context, runTime *runner.Runtime) error {

	const minEngagement = 100
	topics := []string{"AI", "Econ", "Crypto", "Stocks", "World News"}

	// gather
	source := sources.Mock{}
	tweets, err := source.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("fetch from %s: %w", source.Name(), err)
	}

	// process (no tokens) + reason (heuristic, no tokens)
	kept := filter(tweets, minEngagement)
	digest := summarize(kept, topics)
	message := render(digest)

	runTime.Log.Printf("[twitter-digest] %d fetched -> %d kept -> %d buckets", len(tweets), len(kept), len(digest.Buckets))

	if runTime.DryRun {
		runTime.Log.Println("[twitter-digest] dry-run: would have delivered:", message)
	}

	sink := sinks.Console{Out: runTime.Log.Writer()}
	return sink.Deliver(ctx, message)
}
