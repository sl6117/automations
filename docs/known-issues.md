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
- **Retweets distort every engagement number.** X reports an RT with `like_count: 0` and
  `retweet_count` equal to the ORIGINAL post's count, so `Likes+Reposts` scores a retweet by
  someone else's popularity. It clears the `minEngagement` floor easily and wins per-author
  cap slots off content the account did not write. `observe()` records `IsRetweet`, so any
  scoring work should use it.
- **`evalDigest` false-positives on retweet citations.** For an RT the model attributes the
  content to the original author but the status id belongs to the retweeter, so the cited URL
  is `x.com/<original>/status/<retweeter-id>`. `eval.go` compares whole URLs against the kept
  set and reports `hallucinated url`. Harmless downstream: X resolves status URLs by id and
  ignores the handle segment, `citations.go` joins on the numeric id, and the revise loop
  gates on the judge's faithfulness verdict, never on `evalFailures`.
- **@FirstSquawk and @Reuters cannot clear `minEngagement: 100`.** On 2026-07-29 they were
  77 of 241 fetched posts (32% of the spend) with max engagement 97 and 92 — every post
  dropped. Newswire accounts are high-substance and low-engagement, so an absolute floor is
  the wrong instrument for them. @Reuters has never had a kept post in the archive;
  @FirstSquawk has 28 citations at 82.4%, so it is marginal rather than worthless.

## weekly-deepdive

- **Empty briefs are an action-space problem, not a prompt problem.** Researchers must find
  sources but have only `fetch_url` and no search tool, so they invent article slugs;
  findings stay 0/0/0. Two rounds of grounding fixes worked mechanically (X fetches blocked,
  dates current, input tokens down ~27%) and changed nothing downstream. The fix is a search
  `docs/interview-prep/` (private) and the north-star roadmap. Search+allowlist shipped;
  empty briefs were an action-space problem, not a prompt problem.
- **Synthesizer hedge tic (unfiled).** "reported but not corroborated" is wedged
  mid-sentence 15+ times per brief, producing ungrammatical output. The editor passes it
  because it checks contract compliance, not readability.

## infra / CI

- **Schedules:** digest daily 09:00 PT, deepdive Sundays 10:00 PT — both EventBridge, both
  live. Nothing runs locally anymore (no launchd, no crontab). Lambda logs live in
  CloudWatch, which is off-limits per `~/.cursor/rules/no-aws-cli.mdc`.
- **Pushing to `main` does not deploy — this has already cost a day of data.** CI is
  test-only and the Lambdas run a container image baked at `cdk deploy` time, so committed Go
  code keeps running the old behavior on schedule until someone runs
  `cd infra && npm run deploy` with Docker Desktop up. The judge-funnel instrumentation was
  committed 2026-07-28, went unnoticed as undeployed, and the 2026-07-29 run produced another
  day of `Unmeasurable` artifacts. Any bite that changes runtime behavior ends with a deploy.
- **Confirming a deploy landed:** re-run `npm run diff`. The image URI on the `[-]` side is
  what CloudFormation currently holds, so it should show the digest you just pushed. The
  `waiting in review for manual execution (--no-execute)` line during deploy is misleading —
  the `deploying… [1/1]` and `✅` that follow are the real outcome.
- **Both Lambdas share one image.** Deploying for the digest also ships weekly-deepdive code,
  so an unfinished deepdive change on `main` will go live with an unrelated digest deploy.
