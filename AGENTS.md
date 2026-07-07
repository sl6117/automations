# AGENTS.md — context for AI agents working in this repo

This file is the always-loaded "house rules" context. Keep it SHORT. Deep details live in
per-project docs and `docs/decisions/` that are loaded only when a task needs them
(progressive disclosure / context-on-demand).

## What this repo is
A Go personal automation foundation (module `github.com/sl6117/automations`). One runner
binary (`auto`) runs pluggable projects behind a single `Project` interface. Project #1 is
`projects/twitter-digest`: X list -> filter -> Claude Haiku digest -> Telegram/email.

## North star (read this before proposing work)
The owner is growing this framework into an agentic system to master agent orchestration,
loop engineering, and queue engineering — see
`docs/decisions/2026-07-06-north-star-agentic-roadmap.md` for the goal, the build sequence,
and the working agreements. Steer new work toward that roadmap; don't propose throwaway
side projects or feature work that doesn't advance it.

## Where things live
- `cmd/auto/` - the runner CLI: `auto list | run <project> [--dry-run] | cost`.
- `internal/runner/` - the `Project` contract and registry.
- `internal/ai/` - LLM clients (Anthropic, OpenRouter).
- `internal/obs/` - cost logging + report (`auto cost`).
- `pkg/sources/` - data-source adapters (X API, mock). `pkg/sinks/` - delivery (telegram, email, console).
- `projects/<name>/` - one automation: code, `config.json`, `prompts/`, tests.
- `docs/decisions/` - why we did things (archive analysis here, not in code comments).

## Conventions
- Secrets only in `.env` (gitignored, loaded by direnv). Never hardcode keys. Never print secret values.
- Personal data (chat ids, emails, `subscribers.json`) is never committed.
- `AUTOMATION_ROOT` anchors all persistent paths (state, logs, artifacts). Tests must set it: `t.Setenv("AUTOMATION_ROOT", t.TempDir())`.
- Config (non-secret) lives in each project's `config.json`.
- Each pipeline is: fetch -> filter (no LLM) -> summarize (LLM) -> deliver -> log.
- Filtering/dedup happens in plain code BEFORE the model, to minimize tokens.
- Prefer the cheapest capable model (default: Claude Haiku). Escalate only when needed.
- The digest prompt's output format is a contract: `## <English topic>` headers route
  sections to subscribers. Any prompt change must preserve it.

## How to add a new project
Implement `runner.Project` (`Name()`, `Run()`), call `runner.Register` in an `init()`,
and blank-import the package in `cmd/auto/main.go`.


## Running
- Build `go build -o bin/auto ./cmd/auto` - REQUIRED after code changes; launchd runs the prebuilt `bin/auto`, so a stale binary silently runs old behavior.
- Try it: `bin/auto run twitter-digest --dry-run` (no sends, no state writes).
- Never run the mock source without `--dry-run`: it overwrites the real fetch cursor.
- Tests: `go test ./...`.
- Scheduled daily 09:00 via launchd (`scripts/schedule-launchd.sh`); logs in `logs/`.
