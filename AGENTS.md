# AGENTS.md — context for AI agents working in this repo

This file is the always-loaded "house rules" context. Keep it SHORT. Deep details live in
per-project READMEs and `references/` files that are loaded only when a task needs them
(progressive disclosure / context-on-demand).

## What this repo is
A personal automation foundation. Each automation is a self-contained folder under `projects/`.
Shared building blocks live under `lib/`. Project #1 is `projects/twitter-digest`.

## Where things live
- `lib/fetch/`   — data-source adapters behind one interface. Swap sources without touching project logic.
- `lib/deliver/` — delivery adapters (telegram, console).
- `lib/cost-log/`— append-only token/cost log + a report command.
- `projects/<name>/` — one automation: `config.json`, `prompts/`, `references/`, `pipeline.js`, `state.json`.
- `docs/decisions/` — why we did things (archive analysis here, not in code comments).

## Conventions
- Secrets only in `.env` (gitignored). Never hardcode keys. Never print secret values.
- Config (non-secret) lives in each project's `config.json`.
- Each pipeline is: fetch -> filter (no LLM) -> summarize (LLM) -> deliver -> log.
- Filtering/dedup happens in plain code BEFORE the model, to minimize tokens.
- Prefer the cheapest capable model (default: Claude Haiku). Escalate only when needed.

## How to add a new project
Copy `projects/twitter-digest/` to `projects/<new>/`, then swap the fetch adapter and the
prompt. The filter/deliver/log plumbing is reusable as-is.

## Running
- Offline demo (no credentials, no tokens): `npm run digest:mock`
- Real run: fill `.env`, set `FETCH_SOURCE=bird`, `DIGEST_DRY_RUN=0`, then `npm run digest`.
