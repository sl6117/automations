# North star: grow the framework into an agentic system

**Date:** 2026-07-06
**Status:** active — this is the roadmap agents should steer toward

## The goal

The owner is using this repo to master AI-engineering skills by building: agent
orchestration, loop engineering, queue engineering, MCP/tool use, context engineering,
and verification — on top of the harness fundamentals already built here (interfaces,
DI seams, observability, eval, secrets hygiene, scheduling).

The strategy decision: **do not start new throwaway projects and do not restart.** The
twitter-digest project and the runner framework are the substrate. Each new capability
must make the product genuinely better while exercising one of the target skills. Cloud
infra (DynamoDB, Lambda, EventBridge via CDK) is built *in service of* these capabilities,
not as a separate track.

## Why this project can teach these skills

- **Queue engineering** = the shelved correctness bug. A failing subscriber silently
  loses days because the fetch cursor advances anyway. Fix: per-subscriber delivery
  becomes a durable job (pending/delivered/failed, attempt count, backoff, alert after
  N consecutive failures). A claim/lease queue on DynamoDB conditional writes is real
  queue engineering AND real cloud infra at once.
- **Loop engineering** = digest quality. Wrap the single digest() call in
  draft -> LLM-judge critique (coverage, format contract, no hallucinated URLs) ->
  revise -> repeat until pass or budget exhausted.
- **Orchestration + MCP** = the weekly deep-dive project: read the week's digest
  artifacts, pick the biggest story, fan out researcher agents that fetch sources,
  synthesize a long-form brief, editor loop polishes, deliver via existing sinks.
- **Note on a corrected assumption:** orchestration is not primarily load-balancing
  repetitive tasks (that's the queue layer). It is decomposition into roles with
  contracts between them, plus verification — how one agent knows another's output
  is good enough to build on.

## The build sequence

1. ~~Storage abstraction: `storage.Store` interface + filesystem impl~~ (done 2026-07-06)
2. ~~Migrate all four storage call sites (state, artifacts, subscribers, cost log)
   through the Store seam~~ (done 2026-07-08; lesson learned: tests must inject
   `&storage.FS{Root: t.TempDir()}`, never `NewFS()` — ambient AUTOMATION_ROOT
   points at the real repo and a test once polluted the real cost log)
3. CI: GitHub Actions running `go vet` + `go test ./...` on push
4. ~~Delivery queue — fixes the silent-subscriber-loss bug~~ (done 2026-07-09, built
   directly on DynamoDB since it was already live, skipping the "filesystem first" plan:
   `internal/queue` — Queue contract (Enqueue/Pending/Claim/Complete/Fail), Memory +
   Dynamo backends, leases via conditional writes, per-subscriber jobs keyed
   "<newestTweetID>#<subscriber>" for idempotent enqueue, cursor advances after enqueue
   instead of after delivery, every run drains leftovers, dead-letter alert to the
   operator's Telegram after 5 attempts. Proven same-day by a real transient Telegram
   failure: email delivered, the telegram job queued with attempt 1 recorded, then
   retried and delivered on the next run — the exact failure that lost Jul 4-5 digests.
   Lesson learned #3: DynamoDB rejects empty AttributeValues — Memory accepted a nil
   payload, the real API refused it; the integration tests caught what unit tests
   structurally could not.
   Lesson learned #4: draining must not be conditional on producing — the kept==0 early
   return originally skipped the queue entirely, stranding retry jobs on quiet days.)
5. ~~DynamoDB impl of Store~~ (done 2026-07-08: single table `automations` in us-east-2,
   pk = storage key, sk = "_" for blobs / timestamp for appends, project attribute,
   on-demand mode; STORAGE_BACKEND=dynamo selects it; cmd/seed migrates/repairs from
   the filesystem copy). Queue on DynamoDB still pending — see step 4.
   Lesson learned #2: with STORAGE_BACKEND=dynamo in the shell, tests that fall back
   to env-selected storage hit the REAL table and REAL delivery sinks (it happened —
   mock cursor overwrote production state, test digests were delivered to real
   subscribers; repaired by re-running cmd/seed). Every project test must inject
   `store` explicitly, and "no messages arrived anywhere" is part of a passing run.
6. Tier-2 LLM judge over run artifacts (evaluator agent)
7. Generator -> evaluator revise loop using that judge (loop engineering)
8. Weekly deep-dive project (orchestration + MCP + fan-out)
9. Lambda + EventBridge via CDK — "the thing that runs the worker," last, not first

## The portfolio story this produces

"I built an automation framework, then grew it into an agentic system — durable job
queue on DynamoDB, self-correcting generation loop, multi-agent research pipeline."

## Working agreements that persist

- Guided-driver mode: the owner types the code and runs the commands; agents supply
  exact steps, explain the why, and review what was typed.
- Work in bites: one scoped improvement -> tests -> rebuild binary -> commit.
- Cost-conscious: cheapest capable model, filter before the LLM, watch both spend
  meters (Anthropic tokens via `auto cost`; X API credits in the dev console).
