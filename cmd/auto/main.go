// the runner CLI for the personal automation foundation
// usage:
// auto list
// auto run <project> [--dry-run]
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/sl6117/automations/internal/obs"
	"github.com/sl6117/automations/internal/runner"
	"github.com/sl6117/automations/pkg/sources"

	// Project registrations go here as blank imports so their init() runs.
	"github.com/sl6117/automations/internal/storage"
	_ "github.com/sl6117/automations/projects/hello"
	_ "github.com/sl6117/automations/projects/twitter-digest"
	_ "github.com/sl6117/automations/projects/weekly-deepdive"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "list":
		cmdList()
	case "run":
		cmdRun(os.Args[2:])
	case "cost":
		cmdCost()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `auto - personal automation runner
	
	Usage:
		auto list                        list registered projects
		auto run <project> [--dry-run]   run a project
		auto cost                        show LLM cost report
	`)
}

func cmdList() {
	names := runner.Names()
	if len(names) == 0 {
		fmt.Println("No projects registered")
		return
	}
	for _, n := range names {
		fmt.Println(n)
	}
}

func cmdCost() {
	store, err := storage.FromEnv(context.Background())
	if err != nil {
		log.Fatalf("cost: %v", err)
	}

	if err := obs.Report(context.Background(), store, os.Stdout); err != nil {
		log.Fatalf("cost: %v", err)
	}
}

func cmdRun(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "run: missing project name (try 'auto list')")
		os.Exit(2)
	}

	name := args[0]

	fs := flag.NewFlagSet("run", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "describe actions without performing side effects")
	fs.Parse(args[1:])

	project, ok := runner.Get(name)

	if !ok {
		fmt.Fprintf(os.Stderr, "run: unknown project %q (try 'auto list')\n", name)
		os.Exit(1)
	}

	runTime := &runner.Runtime{
		DryRun:     *dryRun,
		Log:        log.New(os.Stdout, "", 0),
		ProjectDir: filepath.Join("projects", name),
	}

	if err := project.Run(context.Background(), runTime); err != nil {
		log.Printf("run: project %q failed: %v", name, err)
		if errors.Is(err, sources.ErrQuota) {
			os.Exit(3)
		}
		os.Exit(1)
	}

}
