// rejudge replays the tier-2 judge over stored run artifacts, fililng in verdicts
// for runs that predate the judge. Usage: rejudge [-force]
package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/sl6117/automations/internal/storage"
	twitterdigest "github.com/sl6117/automations/projects/twitter-digest"
)

func main() {
	force := flag.Bool("force", false, "re-judge artifacts that already have verdicts")
	projectDir := flag.String("project-dir", "projects/twitter-digest", "directory with config.json and prompts/")

	flag.Parse()

	ctx := context.Background()
	store, err := storage.FromEnv(ctx)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	logger := log.New(os.Stdout, "", 0)
	if err := twitterdigest.ReplayJudge(ctx, store, nil, *projectDir, *force, logger); err != nil {
		log.Fatalf("rejudge: %v", err)
	}
}
