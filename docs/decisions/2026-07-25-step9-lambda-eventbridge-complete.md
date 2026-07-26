# Step 9 complete: both automations run in AWS, launchd retired

**Date:** 2026-07-25
**Status:** done — the north-star roadmap (steps 1-9) is finished. AWS is the only
scheduler. Design doc: `2026-07-23-step9-lambda-eventbridge-design.md`.

## What shipped (this arc's commits, oldest -> newest)

- `5db17f5` bite 1: `infra/` CDK app (TypeScript, hand-rolled), account bootstrapped
  in us-east-2, hello Lambda deployed and invoked.
- `cba782e` bite 2: `cmd/auto-lambda` (event `{project, dryRun}` -> `runner.Project`),
  repo-root Dockerfile (arm64, `provided:al2023`, `CMD ["bootstrap"]`), `.dockerignore`
  that excludes `.env`. Proven locally under the Lambda RIE, then in the cloud.
- `ec0b1ed` bite 3: cold-start SSM loader (`GetParametersByPath /automations/`, sets
  process env vars, env-set-on-function wins), Dynamo + SSM grants,
  `STORAGE_BACKEND=dynamo`.
- `522c000` bite 4: `makeWorker` helper, `automations-digest` (10 min) and
  `automations-deepdive` (15 min), EventBridge Scheduler schedules in
  `America/Los_Angeles` — daily 09:00 digest, Sundays 10:00 deepdive — firing
  `dryRun: true` while launchd stayed live.
- bite 5 (this commit): `dryRun: false` on both schedules, both launchd agents removed.

## The cutover, and the evidence for each gate

The dry-run parallel phase was the safety rail: cloud proves every mechanism while
launchd keeps delivering, and nothing can double-send because a dry run cannot deliver
or write state (`project.go` returns before `advanceCursor`).

1. **Scheduled, unattended, timezone-correct.** The 2026-07-25 09:00 digest schedule
   fired at `09:00:26 PDT` with the MacBook asleep: 144 fetched -> 19 kept, tier-1 eval
   19/19 cited in all three languages, dry-run delivery logged, 952 ms, 38 MB of 1024,
   169 ms init. This is what proved `ScheduleExpressionTimezone`, the container, the SSM
   secret load, the X fetch and the Dynamo state read all work with no laptop involved.
2. **Cron shape confirmed at synth time**, not just empirically:
   `cron(0 9 * * ? *)` and `cron(0 10 ? * SUN *)`. The `?` day-of-month slot alongside
   `SUN` is what makes the weekly one actually weekly; checking it in synth output is
   what made it safe to retire the weekly launchd job without observing its first
   scheduled fire.
3. **The flip diff touched exactly two resources**, both `AWS::Scheduler::Schedule`
   target inputs. No Lambda update means no image drift.
4. **Live cloud delivery.** Manual `dryRun: false` invoke: judge passed all three
   languages, Telegram received on phone, five email subscribers delivered, cursor
   written to Dynamo, 46 s of a 10 min timeout, 39 MB of 1024.

Note that the tier-2 judge and the revise loop are gated behind `if !runTime.DryRun`,
so no dry run can ever exercise them. The manual live invoke was the first cloud run to
execute that path — worth doing deliberately rather than discovering it on a scheduled
run.

## Why the cutover happened Saturday instead of Monday

The plan was to observe both weekend dry runs and flip Monday, to keep the double-spend
window short. Saturday morning made the case for going early: the MacBook was asleep at
09:00, so launchd deferred the daily digest to 09:27 when the machine woke, and the run
died immediately — `dial tcp: lookup dynamodb.us-east-2.amazonaws.com: no such host`,
because networking wasn't up yet that early after wake. `run-digest.sh` then sat frozen
inside `sleep 600` (the sleep does not advance while the system sleeps), so the digest
was neither delivered nor failed for four hours. The cloud schedule had fired correctly
at 09:00:26 and, being a dry run, delivered nothing. Two schedulers, no digest.

That is precisely the class of failure the cloud removes, so the remaining gate
(observing Sunday's deepdive dry run) was traded for a synth-output check of its cron.

## Incident during the cutover: one duplicate digest, 2026-07-25

Both today's digests were real sends, 15 minutes apart:

- **13:33 local.** Opening the laptop restored networking and let the frozen
  `sleep 600` expire; attempt 2 succeeded and delivered the full digest (146 fetched ->
  26 kept, Korean faithfulness failure caught and revision adopted), cursor ->
  `2081114802248646798`. This was the last local run ever.
- **13:48 cloud.** A manual live invoke intended to recover the "missing" digest. The
  log had been read at 13:26 while still frozen and assumed to stay that way; it was not
  re-checked immediately before the invoke. It found the 10 tweets from the intervening
  15 minutes, kept 4, and sent a thin second digest to the same six recipients.

Harm was limited to one extra set of X reads, one extra three-language digest, and a
short second email. The shared Dynamo cursor did its job — the second run only sent
because genuinely new tweets existed; with none it would have logged `no tweets to
digest - skipping send`. Cursor ended at `2081119055100940310`, monotonic.

Lesson worth keeping: a wrapper stuck in retry backoff is a *pending* run, not a failed
one, and its state is only valid at the moment you read it. Before any manual recovery
send, re-read the log immediately beforehand, or kill the pending runner first.

## Operating the system now

- Triggers: EventBridge Scheduler only. `automations-digest` daily 09:00 PT,
  `automations-deepdive` Sundays 10:00 PT. Nothing local fires — no plists, nothing
  loaded in launchd, no crontab.
- Deploy: `cd infra && npm run diff && npm run deploy` (Docker Desktop must be running).
- Manual run: `aws lambda invoke --function-name automations-digest --payload
  '{"project":"twitter-digest","dryRun":false}'` (the local CLI is v1, so no
  `--cli-binary-format`).
- Logs: `/aws/lambda/automations-{digest,deepdive}`. Cost log still lands in the same
  Dynamo table, so `auto cost` locally reads cloud runs too.
- `bin/auto` is no longer on any critical path, but keep it for manual local runs and
  `auto cost`.
- Rollback to launchd: `./scripts/schedule-launchd.sh` and
  `./scripts/schedule-weekly-launchd.sh` regenerate and load both plists from the
  current repo path.

## Known residual gaps (deliberately not fixed)

1. **No retries on either schedule.** A retried digest after a partial failure could
   double-send Telegram. Now that the cutover is done, decide per project — the digest
   wants idempotency before retries; the deepdive is cheaper to retry but costs LLM
   spend.
2. **No failure alarm yet.** A scheduled run that errors is currently silent unless the
   logs are read. The digest already has a quota-alert sink; a CloudWatch alarm on
   Lambda `Errors` per function is the obvious next step.
3. **Step 8 researcher residuals** (archive wildcard URLs, empty `sources: []`,
   `corroborated=false` on tweet-only evidence) reproduce in Lambda exactly as
   documented. Still observe-don't-fix.
4. **The deepdive's first scheduled cloud run is Sunday 2026-07-26 10:00 PT** and has
   only ever been proven by manual invoke. If it does not appear in
   `/aws/lambda/automations-deepdive`, recovery is one manual invoke.
