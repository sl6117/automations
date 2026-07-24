# Step 9 design: Lambda + EventBridge via CDK

**Date:** 2026-07-23
**Status:** design agreed with owner; build starting. Read this before touching
`infra/` or `cmd/auto-lambda`.

## The shape

Move both scheduled jobs off launchd and into AWS: EventBridge Scheduler fires a
Lambda that runs the same `runner.Project` code the CLI runs today. The pipeline code
does not change — step 9 builds "the thing that runs the worker," nothing else.

Two new pieces:

1. **`cmd/auto-lambda`** — a thin Lambda entrypoint. Receives an event like
   `{"project": "twitter-digest", "dryRun": true}`, resolves secrets (see below),
   and calls `runner.Get(project).Run(...)` with the same `Runtime` the CLI builds.
   `cmd/auto` stays untouched for local use.
2. **`infra/`** — a TypeScript CDK app in this repo, with its own `package.json`.
   Deploys are `npm run deploy` from `infra/`, always driven by the owner
   (no-aws-cli rule: the assistant never touches AWS directly).

## Decisions (owner, 2026-07-23 design session)

1. **Packaging: container image** (`DockerImageFunction`). The Dockerfile copies the
   exact repo layout — `bin/auto-lambda`, `bin/digest-mcp`, `projects/*/prompts/`,
   `projects/*/config.json` — and sets `WORKDIR` to the bundle root. This preserves
   the two cwd-relative contracts unchanged: `cmd/auto`'s
   `ProjectDir = projects/<name>` and weekly-deepdive's
   `exec.Command("bin/digest-mcp")`. The image is locally testable with `docker run`.
   Cold-start overhead is irrelevant at two invocations a day. Cost: deploys need
   Docker running locally.
2. **Topology: one shared image, two Lambda functions** — one per project, each with
   its own EventBridge schedule, timeout (digest tight, deepdive the full 15 min),
   log group, and failure alarm. A bad deepdive deploy cannot touch the digest. The
   event payload still carries `{"project": ...}` so the handler stays generic; the
   per-function split is pure CDK configuration.
3. **Secrets: SSM Parameter Store SecureStrings** under `/automations/<KEY>`, one per
   key currently in `.env` (ANTHROPIC, X bearer + list ID, TELEGRAM token + chat ID,
   RESEND; OPENROUTER if still used). Standard parameters are free; Secrets Manager's
   $0.40/secret/month and rotation machinery buy nothing here. `cmd/auto-lambda`
   reads the parameters once at cold start and sets them as process env vars, so all
   existing `os.Getenv` call sites work unchanged. The owner seeds parameters from
   `.env` by hand — values never appear in the repo, CDK code, or chat.
4. **CDK app lives in `infra/` in this repo, TypeScript.** One history for app and
   infra; the deploy command lives next to the code it deploys.
5. **Cutover: dry-run in parallel, then a same-day flip.** launchd keeps running
   live. Cloud schedules initially invoke with `"dryRun": true` (no sends, no cursor
   writes), so both coexist safely. After clean dry runs of BOTH jobs are observed in
   CloudWatch, flip the cloud schedules to live and unschedule launchd the same day.
   Never run both live at once: double Telegram sends and a fought-over fetch cursor.

## New dependencies (flagged per house rules)

- Go: `github.com/aws/aws-lambda-go` (official AWS runtime library), AWS SDK v2 SSM
  client (`github.com/aws/aws-sdk-go-v2/service/ssm` — same family as the existing
  Dynamo client).
- npm (scoped to `infra/`): `aws-cdk-lib`, `constructs`, `aws-cdk` CLI, TypeScript.

## What moves to cloud semantics

- **Retries.** The launchd wrapper's `sleep 600` retry cannot exist inside Lambda's
  15-minute cap. Retry policy moves to EventBridge Scheduler (bounded retry with
  backoff). Gotcha to design at wiring time: a retried digest run after a PARTIAL
  failure could re-send Telegram — decide whether retries are safe per project before
  enabling them (deepdive is idempotent-ish per run; digest moves a cursor).
- **Timezone.** EventBridge Scheduler supports `America/Los_Angeles` natively —
  schedule expressions stay in local time, no UTC math, DST handled.
- **Quota signal.** `auto` exits 3 on `sources.ErrQuota`; exit codes don't exist in
  Lambda. The handler returns the error and the distinction lives in structured logs
  (and later, metrics) instead.
- **IAM.** The Lambda role replaces local AWS creds: DynamoDB access scoped to the
  `automations` table (us-east-2) + read access to the `/automations/*` SSM
  parameters. Nothing else.

## Bite sequence

1. CDK skeleton in `infra/` + first deploy of a trivial hello Lambda — proves
   bootstrap, credentials, and the deploy loop before any real packaging.
2. `cmd/auto-lambda` handler + Dockerfile bundling both binaries and project assets;
   prove locally with `docker run`, then deploy and invoke with
   `{"project": "twitter-digest", "dryRun": true}` manually.
3. SSM parameters seeded by owner; handler reads them at cold start; a cloud dry run
   of twitter-digest succeeds end to end (Anthropic + X reachable, Dynamo writes land).
4. Second function + both EventBridge schedules (dry-run payloads); observe one
   scheduled cycle of each in CloudWatch.
5. Flip payloads to live, unschedule launchd (`scripts/unschedule-*.sh`), watch the
   first live cloud runs. Cutover complete; step 9 (and the roadmap) done.

Bites 1-2 are useful standing alone even if the project stalls there.

## Gotchas specific to this work

- Lambda's filesystem is read-only except /tmp. The run path is already clean —
  storage and the cost log go through `storage.Store` and `STORAGE_BACKEND=dynamo` —
  but verify no stray local writes appear when testing in the container (bite 2).
- The container must be built for the Lambda architecture (arm64 recommended,
  cheaper); `GOOS=linux GOARCH=arm64` for both binaries, matched in the CDK function
  definition.
- `--dry-run` semantics are the safety rail for the whole cutover: dry-run forces
  console-only sinks and skips state writes. Any change to that behavior during
  step 9 invalidates the cutover plan.
- Real cloud runs cost real money exactly like launchd runs (X reads bill even on
  dry runs). Don't idly re-invoke; every manual test is spend.
- `docs/handoffs/` stays gitignored; this design doc is the committable record.
