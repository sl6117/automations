# Known issues & standing gotchas

Permanent facts that outlive any single work session. Referenced by handoffs so they
stop being copied forward verbatim into every one. Delete an entry when it is actually
fixed, not before.

## twitter-digest

- **`maxPages` is at 5 for the measurement week.** ~$1.25/day instead of ~$0.75; roughly
  $37.50/month if forgotten, against limited remaining X credit. Lower back to 3 around
  2026-08-04. Config: `projects/twitter-digest/config.json`.
- **AGENTS.md rationale is stale.** The line "Filtering/dedup happens in plain code BEFORE
  the model, to minimize tokens" should say the rationale is now *quality*, not tokens — X
  reads dominate cost. Update once bite 6 (scoring) lands.
- **Cosmetic typos, fix opportunistically.** `pkg/sources/xapi.go`: "malformedvalue", "the
  last oe allowed", "bc", "advnaces", "newst". `projects/twitter-digest/sourcestats.go`:
  "detceted", "whehter". Harmless.
- **`6e985a2` is a broken commit in history** (shipped `project.go` without the matching
  `filter.go`); `git bisect` will trip on it. Do not rewrite pushed history to fix it.
- **Reading real stored rows needs `STORAGE_BACKEND=dynamo`.** Without it the CLI reads the
  local filesystem copy, which the Lambdas never write to.
- **DynamoDB 400KB item limit.** One blob per run under `logs/sourcestats/`; ~250 indented
  rows ≈ 135KB. `MarshalIndent` inflates a machine-read log ~40% for no benefit — switch to
  compact `json.Marshal` if the source list grows.

## weekly-deepdive

- **Empty briefs are an action-space problem, not a prompt problem.** Researchers must find
  sources but have only `fetch_url` and no search tool, so they invent article slugs;
  findings stay 0/0/0. Two rounds of grounding fixes worked mechanically (X fetches blocked,
  dates current, input tokens down ~27%) and changed nothing downstream. The fix is a search
  capability (a design decision), not more prompt tuning. See `docs/ai-techniques.md` §13.
- **Synthesizer hedge tic (unfiled).** "reported but not corroborated" is wedged
  mid-sentence 15+ times per brief, producing ungrammatical output. The editor passes it
  because it checks contract compliance, not readability.

## infra / CI

- **`.github/workflows/ci.yml` has `brnaches: [main]`.** The misspelled key is ignored, so
  the push trigger has no branch filter. Harmless; one-character fix.
- **Schedules:** digest daily 09:00 PT, deepdive Sundays 10:00 PT — both EventBridge, both
  live. Nothing runs locally anymore (no launchd, no crontab). Lambda logs live in
  CloudWatch, which is off-limits per `~/.cursor/rules/no-aws-cli.mdc`.
