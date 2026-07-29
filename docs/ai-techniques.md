# AI techniques, mapped to this codebase

A learning map: each technique, what it actually means, where it lives in this repo, and
what is missing. References are to functions rather than line numbers so they survive edits.

Read this alongside `docs/decisions/2026-07-06-north-star-agentic-roadmap.md`. That file
says where we are going; this one says what we have already built and what it is called
in the wider literature.

---

## 1. Deterministic pre-filtering

Do in plain code anything that does not need a model. Cheaper, faster, testable without an
API key, and it shrinks the context the model has to reason over.

**Here:** `projects/twitter-digest/filter.go`. Engagement threshold, global text dedup, and
a per-author cap all run before a single token is spent.

**Note:** the original rationale was token cost. That has flipped — X reads now dominate
spend, so the filter's real job today is *quality*, not savings. `AGENTS.md` still states
the old rationale.

## 2. Context minimization

The model should see the smallest representation that supports the task. Every extra field
is tokens spent and one more thing to be distracted by.

**Here:** `slimTweets` (used by `buildDigestPrompt`, `buildJudgePrompt`, `buildRevisePrompt`).
`sources.Tweet` deliberately carries only what the digest needs, not raw API payloads.

**Subtle and important:** all three roles serialize through the *same* function, so the
generator, judge, and reviser share one view of the ground truth. A judge that sees
different inputs than the generator is measuring the wrong thing.

## 3. Prompt as contract

When downstream code parses model output, the output format is an API. Change it and you
break callers, so it needs the same care as a schema.

**Here:** `prompts/digest.md` "Output format (strict)". The `## <English topic>` headers
route sections to subscribers, and the `(@handle url)` citation format is what
`parseCitations` now joins on. Two independent consumers depend on that shape.

## 4. Structured output with real validation

Asking for JSON is not the same as getting valid JSON. Small models add prose, markdown
fences, and trailing junk.

**Here:** `extractJSON` in `judge.go` decodes the first complete JSON value rather than
slicing brace-to-brace, which broke on trailing text containing `}`.

**The good bit:** `judgeDigest` unmarshals into a struct of *pointers* and errors if any
dimension is nil. Without that, a missing `faithfulness` key silently becomes
`{Pass: false, Reason: ""}` — a fabricated failing verdict with no explanation. Zero values
are a real hazard whenever you parse model output into a struct.

## 5. Two-tier evaluation: deterministic checks, then a model judge

Cheap mechanical checks catch format and coverage violations. A model judge catches
semantic problems (does this claim follow from the source?) that code cannot.

**Here:** tier 1 is `eval.go`, stored on the artifact as `EvalFailures` / `EvalCoverage`.
Tier 2 is `judgeDigest` in `judge.go`, four dimensions: faithfulness, topic routing,
coverage, clarity.

**Why the order matters:** never spend a model call to detect something a regex can prove.

## 6. LLM-as-judge

A separate model call grades the output against its inputs. The whole discipline of
improving an AI system rests on having a signal, and this is usually the only one available.

**Here:** `judgeDigest`, temperature 0.0 (`judgeTemperature`), on a separately configured
model (`cfg.judgeModel()`), so the judge can be a stronger model than the generator.

**Known limits:** self-evaluation is optimistic, and judge capability varies by language.
Korean fails faithfulness more than English and we do not yet know whether that is a real
quality gap or a judge limitation. See the step-7 gate in the roadmap.

## 7. Generator–critic loop with a verifier gate (reflexion)

Generate, critique, revise, re-verify. The critical design choice is what ships when the
revision fails.

**Here:** `runReviseLoop` in `projects/twitter-digest/revise.go` and the near-identical one
in `projects/weekly-deepdive/revise.go`.

**Adopt-only-if-clean** is the load-bearing rule: an unverified or still-failing revision
never ships, the original does. Without that gate, a revise loop can make output worse
while looking productive. Note this is *not* multi-agent — one model plays three roles and
Go owns every decision.

## 8. Fail-open design

Quality machinery must never block delivery. A broken judge should degrade the product to
"unjudged", not to "nothing was sent".

**Here:** `judgeDigest` errors are logged onto the artifact as `JudgeError` and the digest
still ships. `runReviseLoop` never returns an error at all — any internal failure means the
prior draft ships.

## 9. Artifact archive (replayability)

Store the exact inputs and the exact output of every run. Almost every analysis you will
want later is impossible without this, and impossible to add retroactively.

**Here:** `artifact.go` writes `logs/runs/<ts>-twitter-digest-<language>.json` with the kept
tweets, the digest, token counts, eval failures, and judge verdicts.

**What it has already bought:** `cmd/rejudge` re-grades old runs with a new judge prompt at
no fetch cost, and `citations.go` mined a curation signal out of months of history that
nobody planned for when the archive was designed. Store more than you think you need.

## 10. Offline evaluation and backtesting

Fit and test a change against recorded data instead of buying fresh data for every attempt.

**Here:** `logs/sourcestats/` records *every* fetched post with its metrics and the filter's
verdict, including dropped ones — so a new scoring rubric can be replayed over real history.
`LoadSourceStats` and `Summarize` in `sourcesreport.go` are split as pure functions
specifically so a backtest harness can reuse `Summarize` without touching storage.

**The habit:** separate loading from computing. Pure functions over plain structs are
testable, replayable, and reusable; a function that does both is none of those.

## 11. Implicit feedback signals

Explicit quality labels are expensive. Signals already sitting in your logs are free.

**Here:** `citations.go`. The digest cites `(@handle url)`, so parsing status ids out of
published digests and joining them to each run's kept posts yields a *citation rate*: of the
posts that reached the model, which ones the editor actually used. That is much closer to
real value than a keep rate, which only measures our own filter.

**Design notes worth reusing:** join on the numeric id, never the handle (ids survive
translation and case-folding); count with sets, not counters, because the same run is
published once per language; and distinguish "never asked" (`-`) from "asked and ignored"
(`0.0%`), because conflating them slanders a source. `UnmatchedCitations` is a free
hallucination canary — a cited id that was never in any kept set means an invented URL.

## 12. Tool-use agent loop

This is what "agent" actually means: the model chooses its own next action in a loop, rather
than being called once for text.

**Here:** `internal/agent/agent.go`, `Run`. The model receives a tool set, and each turn it
either calls tools or answers. `MaxToolTurns` bounds the loop and `finalAnswer` forces a
response from whatever was gathered — the escape hatch that stops an agent looping forever.
`Result.Truncated` tells the caller the answer was budget-cut so it can hedge.

**This is the only place in the repo where a model controls flow.** Everything else is Go
deciding and the model filling in text.

## 13. Action-space design (the current failure)

An agent can only do what its tools let it do. When the task requires an action the tool set
cannot express, the model does not stop — it fabricates.

**Here:** `projects/weekly-deepdive/researcher.go`. Researchers must find corroborating
sources but have only `fetch_url`. With no way to search, the only route to a URL is to
guess one, so they invent article slugs. Two rounds of prompt-level grounding fixes worked
mechanically (X fetches blocked, dates corrected, input tokens down 27%) and findings stayed
0/0/0, which is exactly what an action-space problem looks like: prompt changes move
everything except the outcome.

**The general lesson:** encode constraints in the action space, not the prompt. "Do not
invent URLs" is a request. Being unable to fetch a URL that did not come from a search result
or a seed is a guarantee.

## 14. Multi-role pipeline

Distinct prompts with distinct jobs, sequenced by code. More tractable than one giant prompt
because each role is separately testable and separately fixable.

**Here:** `projects/weekly-deepdive` — `planner.go` → `researcher.go` (xN) → `synthesizer.go`
→ `editor.go` → revise loop.

**What it is not:** multi-agent. The topology is fixed at compile time, researchers run
sequentially and cannot see each other's findings, and nothing can spawn work or revise the
plan mid-run. Multiplicity without autonomy.

## 15. Model tiering and temperature discipline

Use the cheapest model that can do the job; reserve stronger models for judging and
escalation. Deterministic tasks get temperature 0.

**Here:** `cfg.Model` (Haiku) for generation, `cfg.judgeModel()` configurable separately,
`judgeTemperature = 0.0` versus a higher `digestTemperature`. `internal/ai` abstracts
Anthropic and OpenRouter behind one `Client` so swapping providers is a config change.

## 16. Cost accounting as a first-class feature

If you cannot see spend per run, you cannot make cost/quality tradeoffs, and you will
discover the bill instead of choosing it.

**Here:** `internal/obs` logs tokens per run; `auto cost` reports. Second meter: `auto sources`
apportions X read spend per handle. Two meters because there are two independent cost
drivers, and X reads now dominate.

## 17. Idempotency and durable work

Anything that can run twice will run twice: retries, redeliveries, overlapping schedules.

**Here:** `internal/queue`. Lease-based exclusive `Claim` with conditional writes so racing
workers cannot both win, attempt counting, final-fail, idempotent `Enqueue` that will not
resurrect a settled job. Same guarantees in the memory and DynamoDB backends.

**Also:** `saveSourceStats` keys blobs by timestamp rather than date, so a same-day retry
cannot overwrite the earlier record.

---

## Not built yet (roughly by value)

**Best-of-N with verifier selection.** Sample the generator N times independently, judge
each, ship the best. Not multi-agent — a loop plus a selector, and the selector already
exists. Attractive here specifically because generation is nearly free next to X reads.
Decorrelates failures in a way that revising an anchored bad draft cannot.

**Retrieval / search.** The blocker for the deepdive (see 13). Search-then-fetch with an
invariant that only search-returned or seed URLs may be fetched.

**Parallel fan-out / fan-in.** Researchers run sequentially today. An `errgroup` buys
latency, not quality or cost, and is the natural first taste of orchestration.

**Orchestrator–worker.** A planner that reads worker results and decides what to do next —
reformulate a failed question, spawn a follow-up, drop a dead end. The first design here
that would honestly be called multi-agent. Blocked on search: without it an orchestrator
would just re-issue impossible questions.

**Checkpointing / resumable runs.** The one idea worth stealing from the graph frameworks:
persist state between steps so a run can resume mid-flight rather than restarting. Most
valuable for the deepdive, where researchers burn many tool turns before the synthesizer
starts. `internal/queue` plus DynamoDB is already the substrate.

**Semantic dedup.** Dedup today is exact string match after normalization, so any wording
difference escapes it. Embeddings would catch near-duplicates, at the cost of a new
dependency and a similarity threshold to tune.

**Human feedback capture.** No path exists for "this bullet was good/bad" to re-enter the
system. Citation rate is a proxy for the model's judgment, not yours.
