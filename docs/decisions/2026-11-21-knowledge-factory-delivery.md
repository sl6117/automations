# Knowledge-factory delivery: sealed files to a private GitHub repo (M5)

**Status:** built on branch `kf/05-schedule`, with publishing off (`projects/knowledge-factory/config.json`). The Lambda registration and the third function and schedule are a patch awaiting the owner's explicit yes, because they touch protected paths. Turning everything on is in `docs/setup/knowledge-factory.md`.

## Why GitHub

The consumer is another team's bot that works through GitHub and cloud agents. It cannot read the AWS table, call `digest-mcp`, or reach an S3 bucket in this account. A private data-only repo (`sl6117/valhalla-knowledge`) is the one place both sides can reach. The repo also gives the desk a commit history for free.

It is written with the plain GitHub REST Contents API from the standard library. That is one file per commit, which is plenty for six small files a day, and it adds no dependency.

## Why only entity_day and the manifest are delivered

After reviewing the contract, the desk said it reads only `entity_day`. The per-post `mentions` file carries account handles and post ids, and the desk's auditor has to review everything that lands in the repo it reads. So `mentions` is still sealed and kept in the corpus store, but it is published only when `publish.includeMentions` is true; it defaults to and ships as false. The manifest is unchanged by this setting, so the seal does not depend on delivery: the delivered manifest names a mentions file the repo does not hold. "Already published" still keys on the manifest, so a revision published without mentions is complete. Switching the setting on later does not backfill earlier revisions.

## Why a sealed file is never overwritten

The contract promises that a sealed revision never changes. That is what makes point-in-time research honest: a backtest that read `r1` at 17:30 must see the same bytes tomorrow. So:
- Publishing only creates. It sends no `sha`, so GitHub refuses to replace an existing file.
- An existing path is compared by git blob sha. Identical bytes are skipped. Different bytes fail the run loudly and are left alone, because a changed sealed file is a bug to investigate, not something to repair silently.

A revision is written data files first and manifest last, so a manifest in the repo means the revision is complete.

## Why catch-up never re-seals

Each run seals today. It then looks back 3 days and seals only days that have run records and no sealed revision, which covers a day the job missed.

It never re-exports a day that already has a revision. If it did, every edit to `universe.json` or to the matching rules would mint a new revision of every recent day on the next run. Re-sealing a past day is a deliberate act: `auto kf export --date D`.

## Why publishing is off by default

Turning it on needs things that do not exist yet: the repo, a fine-grained token in SSM, and a deploy. The job also shares an image with the digest, so merged code ships with the next unrelated deploy. With the flag off, the job seals into the corpus table and makes no network request at all.

A publish failure fails the run, so the Lambda invocation shows as failed, and sends one operator alert. The token is redacted from the error, the log and the alert. Sealing has already happened by then, and the next run publishes what was missed.
