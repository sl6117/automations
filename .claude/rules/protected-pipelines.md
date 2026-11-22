---
paths:
  - "projects/twitter-digest/**"
  - "projects/weekly-deepdive/**"
  - "pkg/sources/**"
  - "pkg/sinks/**"
  - "internal/queue/**"
  - "cmd/auto-lambda/**"
  - "cmd/digest-mcp/**"
  - "infra/lib/automations-stack.ts"
  - "Dockerfile"
---

# Protected pipelines: the daily digest and the weekly deepdive

You are looking at a file that the two live pipelines depend on. They deliver to real
subscribers every day, and a past mistake here cost a day of data. The owner's rule
(2026-11-21) is that they must not break and should not be touched: new work is built
around and over them, reading what they already store.

Before editing anything under these paths:

- Stop and get the owner's explicit yes in this conversation. An earlier approval for a
  different change does not carry over.
- Say what a subscriber could see differently, even if the answer is "nothing", and how
  you know.
- Say that the change is branch-only, that nothing deploys, and that `git switch main`
  undoes it.

If the owner approves:

- Put new behaviour behind a config flag that defaults to off. Both Lambdas share one
  image, so merged code ships with the next unrelated deploy.
- Make it fail-open: a failure in the new code logs and lets delivery proceed.
- Leave what the model sees alone. The request sent to X, the prompts, `slimTweets`
  output and the `## <Topic>` / `(@handle url)` format are contracts. A test pins the X
  request; do not relax it to make a change pass.
- Prove it by comparison: the same input on `main` and on the change, with both outputs
  shown.

Reading these files, and adding tests that do not change behaviour, needs no approval.
