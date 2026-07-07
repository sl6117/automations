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
2. Migrate all four storage call sites (state, artifacts, subscribers, cost log)
   through the Store seam — in progress
3. CI: GitHub Actions running `go vet` + `go test ./...` on push
4. Delivery queue (filesystem Store first) — fixes the silent-subscriber-loss bug
5. DynamoDB impl of Store + queue (single table, pk = storage key, sk = timestamp
   for appends, project attribute for future GSIs; on-demand mode)
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
