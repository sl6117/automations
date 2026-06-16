// defines the contract that every automation implements
// also the registry and dispatch the CLI uses to run them
package runner

import (
	"context"
	"log"
)

// Runtime carries the state and configuration for a single automation run (what a project needs while it runs)
// runner builds 1 runtime per invocation and passes it into Run
// Addint a field here -> makes it available to e ery project at once (as this is a template for all projects)
type Runtime struct {
	DryRun bool
	Log    *log.Logger
}

// Project is the contract every automation satisfies
// All runner knows is these 2 methods
// doesn't care about the internals of the project, just the contract
type Project interface {
	Name() string
	Run(ctx context.Context, rt *Runtime) error
}
