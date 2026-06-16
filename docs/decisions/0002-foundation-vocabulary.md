# 0002 - Foundation vocabulary

Date: 2026-06-16

The terminology behind the Go automation foundation, captured so the design is
legible later. This file is reference-only: it is never loaded into any LLM call
at runtime, so it costs nothing per automation run.

## What this thing is
A small **plugin-based application framework** for personal automations, with a
single **task runner** (`cmd/auto`). Each automation is a "project" that plugs in
behind one contract. "Summarize my Twitter" is just the first tenant.

## Core terms

- **Library vs framework.** A library is code you call. A framework calls *your*
  code. Ours is a framework: the runner calls each project's `Run`.
- **Inversion of Control (IoC) / Hollywood Principle.** "Don't call us, we'll call
  you." The defining trait of a framework. Projects never call the runner; the
  runner invokes them.
- **Harness.** The narrower wrapper that sets up an environment, runs a unit, and
  tears down (e.g. a test harness). In our design this is the `Runtime` + lifecycle
  execution. A harness is a *subset* of the framework: `harness ⊂ framework`.
- **Registry pattern.** A single `name -> Project` map (`internal/runner/registry.go`)
  that is the source of truth for which projects exist.
- **Strategy pattern / program to an interface.** Every project satisfies the same
  `Project` interface; the runner depends on the interface, not concrete types.
- **Implicit interface satisfaction (Go).** No `implements` keyword. A type satisfies
  an interface structurally, just by having the right methods. Verified by the
  compiler where the type is passed as the interface (at `runner.Register`).
- **Self-registration via `init()` + blank import.** Importing a project package
  (even as `_ "..."`) runs its `init()`, which calls `runner.Register`. That is how a
  project plugs itself in without the runner hard-coding a list. Add a project = new
  package + one blank-import line.
- **Dependency injection (DI).** The runner builds a `Runtime` (DryRun, Log, later
  Config/AI/CostLog) and hands it to each project. Projects receive capabilities
  instead of fetching globals. This is also what makes them unit-testable (inject a
  buffer logger).
- **Scaffold / scaffolding.** The reusable starting structure of a project folder you
  copy to make a new one (`intent.md`, `config.json`, `project.go`, `project_test.go`).

## The reusable lifecycle
Each project composes only the stages it needs:
`gather -> process (no tokens) -> reason (optional LLM) -> act -> verify -> log`.
The foundation provides each stage as a building block; per project you supply only
the unique ~10% (the spec, the config, the `Run` wiring, the checks).

## Per-project file types (don't conflate them)

- `intent.md` - the spec (what / why / success criteria). For humans + dev agents.
  Not read by the program. NOT an LLM system prompt.
- `prompts/*.md` - (future, LLM projects only) the actual instructions sent to the
  model at runtime.
- `config.json` - machine-readable **input** knobs the program reads. Never the output.

## Dry-run
`Runtime.DryRun == true` means a project must *describe* what it would do and perform
no side effects (no sends, no writes). The verification/safety contract.
