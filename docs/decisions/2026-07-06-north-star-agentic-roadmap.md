# North star: grow the framework into an agentic system

**Date:** 2026-07-06
**Status:** build sequence COMPLETE (steps 1-9, finished 2026-07-25). This doc stays as the
standing statement of intent and working agreements; active work now comes off the idea
shelf below, currently the input-quality arc (curate the X list, engagement rubric,
corroboration labelling).

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
3. ~~CI: GitHub Actions running `go vet` + `go test ./...` on push~~ (done —
   `.github/workflows/ci.yml`; note the `brnaches:` typo means the push trigger has no
   branch filter and CI runs on every branch)
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
6. ~~Tier-2 LLM judge over run artifacts (evaluator agent)~~ (done 2026-07-13: judge on
   every live digest per language + rejudge replay tool + human calibration read-through
   of all 15 artifacts — see `2026-07-13-judge-calibration-readthrough.md`.
   Lesson learned #5: an uncalibrated judge is just a second opinion. Scoring 30 verdicts
   against the owner separated rubric bugs (fixed free, in prompt text: concrete decision
   rules, closed-world failure rule, judge must know the generator's full contract) from
   capability limits (self-contradicting verdicts, Korean misreads), which justified an
   asymmetric setup: Haiku drafts, Sonnet judges via `judgeModel` config. Judge FAILS are
   high-precision gate material for step 7; judge PASSES are weak evidence, don't gate on
   them.)
7. Generator -> evaluator revise loop using that judge (loop engineering)
   (built 2026-07-17, shipped dark behind reviseBudget=0; enable after live runs show
   no false-positive faithfulness fails. Follow-on idea, owner 2026-07-18: adaptive
   revision prompts — critique history across attempts, escalate the reviser model on
   attempt 2. Gated on first measuring the static loop under budget=1: adoption rate
   and re-judge pass rate must justify the added machinery.)
8. Weekly deep-dive project (orchestration + MCP + fan-out)
   (absorbs owner idea 2026-07-18: verify stories beyond trusting the posting account.
   Scoped as corroboration, not truth-verification — researcher agents fetch linked
   articles/external sources to corroborate the story before the brief asserts it.
   Design agreed 2026-07-20 — see `2026-07-20-step8-weekly-deepdive-design.md`:
   own MCP server over the digest archive + orchestrator-as-host; hedge-don't-drop;
   gates verify contract compliance, not truth.)
9. ~~Lambda + EventBridge via CDK — "the thing that runs the worker," last, not first~~
   (done 2026-07-25 — see `2026-07-25-step9-lambda-eventbridge-complete.md`: CDK app in
   `infra/`, one container image bundling both binaries and project assets, two Lambdas
   with least-privilege IAM, secrets from SSM at cold start, EventBridge Scheduler in
   America/Los_Angeles, launchd retired.
   Lesson learned #6: the cutover ran dry-run-in-parallel first so every mechanism was
   proven in the cloud before anything could send — but a dry run cannot exercise code
   gated behind `if !runTime.DryRun` (the tier-2 judge and revise loop), so the first live
   invoke was still a first. Prove those paths deliberately, not on a scheduled run.
   Lesson learned #7: a wrapper stuck in retry backoff is a PENDING run, not a failed one;
   `sleep` does not advance while the machine is asleep. Re-read the log immediately before
   any manual recovery send — a stale read caused one duplicate digest on 2026-07-25.)

## Idea shelf (approved, ordered)

Capstones first (portfolio freeze after these), then orchestration depth, then ops:

1. ~~Single-source labeling~~ (owner 2026-07-18; DONE 2026-08-02, phase 4 of the
   input-quality arc): bullets whose citations trace to one distinct voice get a
   "[single-source]" marker at the delivery boundary only — artifact/eval/judge/rejudge
   keep the model's unlabeled text. Counting is by status id mapped to kept posts
   (handles get mangled by the model, ids do not); retweets attribute to the original
   author so two RTs of one post are one voice. Annotate only, never suppress.
   Known limit: a bullet whose citation sits on a continuation line gets no label
   (fail-safe; only the offline heuristic renderer emits multi-line bullets).
2. **Ranking-budget filter** (input-quality phase 3 — **FROZEN offline 2026-08-06**):
   `rank.go` / `backtest.go` + `auto rank-backtest`. Pure rank-only and hybrid re-rank
   hurt citation recall; baseline-seeded add-only preserved 100% cite recall but only
   recovered 8 newswire posts across 10 runs (budget already full most days). Do not
   wire into live `filter.go` unless `digestBudget` / list shape changes. Portfolio
   value is the offline eval loop, not a production swap.
3. **RAG-lite** (DONE 2026-08-07): lexical retrieve over English digest artifacts →
   top‑K sections injected into researcher prompts (`retrieve.go`). No embeddings;
   archive text is hint-only — web corroboration still required for `corroborated=true`.
4. **Parallel researcher fan-out** (DONE 2026-08-08) (latency skill): same fixed DAG, run researcher
   roles concurrently with bounded concurrency — production orchestration practice
   without changing topology. Depends on ranking-budget/RAG-lite not blocking; tools
   already solid enough.
   `researchFanOut` runs capped questions concurrently (fail-fast cancel, `Allowlist.Clone`, serialized MCP
   via `serialTools`). Same fixed DAG - latency skill, not topology change. Commit `9e96ecf`.
5. **Critic-triggered replan** (IN PROGRESS 2026-08-08): industry name for the
   production-conservative step after a fixed multi-role DAG + generator–critic
   revise — also “plan-and-execute + replan” / adaptive research (CRAG-style for
   questions). After editor fail, host may call a replan role to **add** 0–N research
   questions, fan-out, append reports, synth/edit again, then text revise. v1 is
   append-only + budgets (`replanBudget`, `maxReplanQuestions`); not an open swarm.
   Design: `docs/decisions/2026-08-08-deepdive-critic-triggered-replan.md`.
6. Dead-letter requeue tooling (`auto queue ls` / `requeue`) — queue operations depth;
   needs a careful fresh session, touches production queue data.
7. Video/linked-media enrichment (post-roadmap).

## The portfolio story this produces

"I built an automation framework, then grew it into an agentic system — durable job
queue on DynamoDB, self-correcting generation loop, multi-agent research pipeline."

## Working agreements that persist

- Guided-driver mode: the owner types the code and runs the commands; agents supply
  exact steps, explain the why, and review what was typed.
- Work in bites: one scoped improvement -> tests -> rebuild binary -> commit.
- Cost-conscious: cheapest capable model, filter before the LLM, watch both spend
  meters (Anthropic tokens via `auto cost`; X API credits in the dev console).
