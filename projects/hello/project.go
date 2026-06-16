// simple automation hello world
package hello

import (
	"context"

	"github.com/sl6117/automations/internal/runner"
)

func init() {
	runner.Register(&project{})
}

type project struct{}

func (p *project) Name() string { return "hello" }

func (p *project) Run(ctx context.Context, runTime *runner.Runtime) error {
	if runTime.DryRun {
		runTime.Log.Println("[hello] dry-run: greet the world!")
		return nil
	}

	runTime.Log.Println("[hello] real: greet the world!")
	return nil

}
