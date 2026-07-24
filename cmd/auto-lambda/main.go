// auto-lambda is the Lambda entrypoint for the automation runner: EventBridge
// (or a manual invoke) sends {"project": "...", "dryRun": bool} and the handler
// runs that project exactly as `auto run <project>` would.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/sl6117/automations/internal/runner"

	// Project registrations, mirroring cmd/auto/main.go.
	_ "github.com/sl6117/automations/projects/hello"
	_ "github.com/sl6117/automations/projects/twitter-digest"
	_ "github.com/sl6117/automations/projects/weekly-deepdive"
)

type Event struct {
	Project string `json:"project"`
	DryRun  bool   `json:"dryRun"`
}

func handle(ctx context.Context, ev Event) error {
	project, ok := runner.Get(ev.Project)
	if !ok {
		return fmt.Errorf("project %q not found", ev.Project)
	}
	rt := &runner.Runtime{
		DryRun:     ev.DryRun,
		Log:        log.New(os.Stdout, "", 0),
		ProjectDir: filepath.Join("projects", ev.Project),
	}
	return project.Run(ctx, rt)
}

func main() {
	lambda.Start(handle)
}
