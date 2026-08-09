# Stack & AI techniques

Public map of what this repo is built from and which AI-engineering ideas it exercises.
Deeper design history lives in [`docs/decisions/`](decisions/). Agent working rules: [`AGENTS.md`](../AGENTS.md).

## Software stack (puzzle pieces)

| Piece | Role here |
|-------|-----------|
| **Go** | One runner (`auto`), pluggable `Project`s, shared libraries under `internal/` / `pkg/` |
| **AWS Lambda** | Scheduled workers (digest daily, deepdive Sundays) — container image, not zip |
| **EventBridge Scheduler** | Cron in `America/Los_Angeles`; replaces local launchd/cron |
| **DynamoDB** | `Store` backend for artifacts, state, cost log; claim/lease **delivery queue** |
| **CDK (TypeScript)** | Infra as code under `infra/` — `npm run deploy` ships the image (**push ≠ deploy**) |
| **SSM Parameter Store** | Secrets at Lambda cold start (never in git) |
| **X API v2** | List timeline fetch (paginated); reads are the dominant $ meter |
| **Anthropic API** | Haiku generate / Sonnet judge & editor; OpenRouter optional via same `Client` |
| **MCP** | `cmd/digest-mcp` — same tool surface for Cursor IDE and the deepdive Lambda host |
| **Telegram + Resend** | Delivery sinks; subscriber routing from gitignored `subscribers.json` |

**Local vs prod:** CLI defaults to filesystem under `AUTOMATION_ROOT`. Real archive reads need `STORAGE_BACKEND=dynamo`. CI runs `go test` only.

```
EventBridge ──► Lambda (shared image)
                   │
                   ├─ twitter-digest / weekly-deepdive
                   ├─ DynamoDB (store + queue)
                   ├─ Anthropic (LLM)
                   └─ X API / Telegram / email
```

## Pipeline shapes

**Daily digest:** fetch → deterministic filter → Haiku digest (prompt cache) → tier-1 eval → Sonnet judge → adopt-only-if-clean revise → enqueue → drain.

**Weekly deepdive:** MCP archive → planner → researchers in parallel (`researchFanOut`, `web_search` + allowlisted `fetch_url`) → synthesizer → editor → (optional **critic-triggered replan**: more research → synth/edit) → text revise → deliver. Host-orchestrated multi-**role** DAG with a bounded conditional edge — not an open agent swarm.

## AI techniques (literature ↔ code)

| Technique | Where |
|-----------|--------|
| Deterministic pre-filter | `projects/twitter-digest/filter.go` (+ offline `auto rank-backtest`; live ranking swap frozen) |
| RAG-lite (lexical retrieve) | `projects/weekly-deepdive/retrieve.go` — top‑K English digest sections into researcher prompt |
| Parallel researcher fan-out | `research_fanout.go` + `serial_tools.go` — concurrent research, fail-fast, MCP mutex |
| Context minimization | `slimTweets` — shared ground truth for digest / judge / revise |
| Prompt as contract | `## Topic` headers + `(@handle url)` citations |
| Structured output | Forced submit tools / agent `OutputTool` + JSON Schema; Go validates |
| Two-tier eval | `eval.go` then LLM judge |
| LLM-as-judge | Sonnet, temp 0, separate `judgeModel` |
| Generator–critic / Reflexion | `revise.go` (digest + deepdive), **adopt-only-if-clean** — text refine, same evidence |
| Critic-triggered replan | Deepdive (building): editor fail → add research Qs → fan-out → append → synth/edit; see [decision](decisions/2026-08-08-deepdive-critic-triggered-replan.md) |
| Fail-open quality | Judge/revise errors never blank the inbox |
| Artifact archive / replay | `logs/runs/…`, `cmd/rejudge` |
| Offline eval / backtest | `sourcestats` + `auto rank-backtest` (citation recall) |
| Implicit feedback | Citation rate via `citations.go` |
| Tool-use agent loop | `internal/agent.Run` — only place the model chooses the next action |
| Action-space design | Allowlist / `GatedFetch` — inventing URLs is impossible, not “discouraged” |
| Multi-role orchestration | `weekly-deepdive` host-owned DAG (+ conditional replan edge) |
| MCP (M clients × N tools) | One server for IDE + orchestrator |
| Prompt caching | Digest system-block breakpoint; Chat automatic cache; priced write/read |
| Model tiering | Haiku default; Sonnet for judge/editor |
| Cost accounting | `internal/obs` + X read meter (`auto sources`) |
| Durable queue / leases | `internal/queue` — idempotent jobs, dead letter |
| Storage seam | `internal/storage` FS ↔ Dynamo |

## Cost & safety habits

- Two meters: LLM tokens (incl. cache) and X page reads.
- Filter / budgets before the model; escalate model only when needed.
- Secrets in `.env` / SSM; personal subscriber data never committed.
- Tests inject stores — never ambient prod Dynamo or live sinks.

## What’s next (idea shelf)

Ordered in the [north-star roadmap](decisions/2026-07-06-north-star-agentic-roadmap.md): ranking-budget (frozen) → RAG-lite (done) → parallel fan-out (done) → **critic-triggered replan** (building) → queue ops tooling. Further replan depth (selective trigger, drop/replace Qs, open supervisor): [decision doc](decisions/2026-08-08-deepdive-critic-triggered-replan.md).
