# Step 8 complete: weekly-deepdive shipped and scheduled

**Date:** 2026-07-23
**Status:** done — roadmap step 8 (orchestration + MCP + fan-out) is live

## What shipped (this arc's commits, oldest -> newest)

- `341845a` agent OnToolCall observability -> `1f3dfa2` planner -> `399a30c` fetch_url
  -> `0cb8622` researcher + 20KB fetch cap -> `6dbaf32` synthesizer + Chat.Text fix
  -> `f781a53` editor -> `890f5b5` render + console delivery
- `efcd694` config.json + Telegram delivery via selectSinks (dry-run always forces
  console only — a dry run can never send)
- `fec4906` researcher seeding: the host resolves planner `sourceTweetIDs` back to
  full tweets via `list_runs`/`get_artifact` (plain code, zero LLM tokens) and injects
  text + embedded links into researcher prompts; prompt forbids invented URLs
- `a4f284f` revise-on-editor-fail loop: `reviseBudget` in config (shipped at 1),
  adopt-only-if-clean, never errors — fail-open like delivery itself
- weekly launchd: `com.thomaslee.weekly-deepdive`, Sundays 10:00 (after the 9:00
  daily digest so the week's final artifact exists; no job overlap).
  Install `scripts/schedule-weekly-launchd.sh`, rollback `scripts/unschedule-weekly-launchd.sh`,
  wrapper `scripts/run-weekly-deepdive.sh` (2 attempts only — each retry is real spend).

## Live proof (2026-07-23, launchctl kickstart)

Full conveyor under launchd: plan -> seed 7/7 tweets -> 3 researchers -> synthesizer
-> editor pass -> **delivered via telegram** (received on phone) **and console**.
Researchers fetched the real t.co seed links first; per-researcher input tokens
roughly halved vs the pre-seeding runs (76-160K vs 154-294K).

## Measured effect of seeding (why bite 2 was worth it)

Before: researchers invented SERP/article URLs, burned ~250K input tokens per
question, produced zero findings. After: findings grounded in seed tweets with
correct attribution, hedged by the synthesizer, editor passes. `corroborated=false`
on tweet-only evidence is CORRECT behavior (a tweet repeated is not corroboration).

## Known residual gaps (observe on scheduled runs before fixing)

1. Researchers return `sources: []` even when findings came from fetched pages —
   candidate one-line prompt fix: "list every URL you actually fetched and relied on".
2. Researchers still invent `web.archive.org/web/*/...` wildcard queries (useless
   23KB search pages). The "same URL via archive" instruction is interpreted loosely.
3. The revise loop is unit-tested but not yet exercised live (editor passed every
   run so far). First natural editor fail will exercise it; check the log for
   `revise loop done`.
4. True corroboration needs a real search capability (the researcher can only fetch
   known URLs). Deliberately out of scope for step 8.

## Ops notes

- Rebuild BOTH binaries after code changes: `bin/auto` AND `bin/digest-mcp`
  (the project spawns `bin/digest-mcp` relative to repo root; a stale server
  silently serves old tools).
- Weekly log: `logs/launchd-weekly-deepdive.log`.
- Manual trigger: `launchctl kickstart -k gui/$(id -u)/com.thomaslee.weekly-deepdive`
  — NOTE this is a real run: real spend, real Telegram send.

## Next

Roadmap step 9: Lambda + EventBridge via CDK ("the thing that runs the worker").
Storage/queue are already on DynamoDB; remaining work is packaging (both binaries
+ prompts/config assets), schedules, and secrets via SSM/Secrets Manager.
Owner drives CDK; never the aws CLI.
