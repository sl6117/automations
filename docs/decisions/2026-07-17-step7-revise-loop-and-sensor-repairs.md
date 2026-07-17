# Step 7 bites 0-1: sensor repairs and the revise loop (shipped dark)

Date: 2026-07-17
Status: loop shipped dark (`reviseBudget: 0`); bite 2 (enable) pending rubric measurement.

## Context

Step 7 of the north-star roadmap is a draft -> judge -> revise loop around `digest()`
(loop engineering). The Jul 16 spot-check of live Sonnet verdicts (Jul 14-16) measured
faithfulness fail precision at **2/4** - too noisy to gate a control loop on:

- TRUE: Jeffries miscitation (Jul 14 KO), "Thunder/Shredded" transcription error (Jul 16 KO).
- FALSE: Buffett (Jul 14 EN) - judge misquoted the source ("his stake" vs the actual
  "entire remaining stake") then argued from outside knowledge; confirmed false against
  the artifact. Russian (Jul 16) - reason concluded "all facts check out" but the verdict
  said fail: the JSON skeleton emitted `pass` before `reason`, so the model committed the
  verdict before reasoning.

Lesson that shaped everything below: **you can't build a control loop on a noisy sensor.**

## Bite 0 - judge hardening (prompt-only, `90b0ca0`)

1. JSON skeleton reordered: `reason` before `pass` per dimension (think-then-decide;
   kills the Russian self-contradiction class). Go parsing is key-based - no code change.
2. Grounding rule: failing faithfulness requires quoting the exact source text and digest
   text side by side; no contradicting quote = pass; outside knowledge forbidden
   (kills the Buffett class).

### Fallout and fixes (the sensor repairs)

- Jul 17 run: **all 3 judge calls truncated mid-JSON** ("unexpected end of JSON input").
  Reason-first + verbose reasons blew through `judgeMaxTokens = 500`. Raised to 1500 and
  added an 80-word per-reason bound (`fb8d63d`). Lesson: a prompt and its decode limits
  are one system - the output contract broke because we changed one without the other.
  Delivery was unaffected: judge stays observability-only (invariant held).
- `cmd/rejudge` refused to retry the truncated artifacts: it treated `JudgeError` as
  "already judged". Fixed so only real verdicts skip; errored attempts are retryable
  (`96f4b60`, test re-pinned). Queue-engineering distinction: attempted != completed.
- Rejudged artifacts exposed a **false positive in the deterministic eval**: the digest's
  merged-citation format `(@a, url1; @b, url2)` - required by digest.md - broke the URL
  regex, which didn't stop at `;`, producing a phantom "hallucinated url" on Jul 17 KO.
  Fixed + pinned (`27fbc05`). Lesson: plain-code checks are sensors too; when a
  deterministic check and an LLM judge disagree, check the artifact before trusting either.
- First verdicts under the new rubric (4 rejudged artifacts): all pass, reasons carry
  verbatim quote pairs and per-dimension discipline. No Buffett-class behavior observed.
- Watchlist: the grounding rule's closed-world phrasing could in principle excuse a
  digest citation whose URL exists nowhere in the sources (nothing to quote against).
  Not observed in practice; do not patch speculatively.

## Bite 1 - the loop (`28b8a7e` plumbing, `7d58250` wiring)

Design decisions:

- **Gate on faithfulness only.** Routing/coverage/clarity are advisory: every routing
  fail scored during calibration and since was a defensible-either-way call.
- **Adopt-only-if-clean.** Revise -> re-judge; the revision ships only if faithfulness
  now passes. Still-failing or errored revisions are discarded and the original draft
  ships. The loop can improve delivery but never block it
  (TestJudgeFailureNeverBlocksDelivery unchanged; 5 new loop-contract tests).
- **Reviser = generator model (Haiku)**; the Sonnet critique reason becomes the revision
  prompt's instructions. `prompts/revise.md` tells the reviser to verify each criticism
  against sources and keep original text where the critique is wrong - defense in depth
  against false-positive critiques.
- `reviseBudget` (config, default 0 = off) caps attempts per language per run; all
  revise/re-judge tokens roll into the run total. Dry-run never enters the loop.
- Loop lives in `runReviseLoop` (revise.go), unit-testable with a struct-literal Config;
  `Run` calls it in one block (project.go ~line 187).

Prompt-contract note: revise.md is now the THIRD prompt sharing digest.md's output
contract (with judge.md). Any contract change must update all three.

## Bite 2 - enabling (pending)

Flip `reviseBudget` to 1 after a few consecutive live runs (Jul 18+) show no
false-positive faithfulness fails under the new rubric. Config is runtime-read: no
rebuild, just the config edit + commit. Then observe a week before considering
topicRouting gating.

## Also this arc

- `scripts/run-digest.sh` retry loop widened to 6 attempts x 10 min (`62de780`) after the
  4th straight day of DynamoDB-unreachable retries. Timestamps showed `sleep 60`
  stretches over laptop sleep, so the real failure class is "offline for hours";
  wider spacing rides it out. Step 9 (Lambda) retires the class.
- Two email subscribers added Jul 16 (one Russian - first non-English/Korean language;
  per-language generation scaled automatically). Each new language costs one Haiku
  digest + one Sonnet judge call per day.
