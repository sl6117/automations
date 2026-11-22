# Knowledge factory: turning it on (owner runbook)

This takes the knowledge factory from "built, everything off" to "the desk receives sealed files every day". Do the steps in order. Each step says how to confirm it worked and how to undo it.

Background you will want in six months:
- **What runs:**
  - The digest (09:00 PT) writes every fetched post to the corpus table when its flag is on.
  - The `automations-knowledge-factory` function (09:30 PT) seals that day's desk feed from the corpus. It catches up any of the previous 3 days that were never sealed. When its own flag is on, it commits the sealed files to the private repo.
- **Nothing reaches the desk until both flags are on and deployed.** Both flags live in `config.json` files that are baked into the container image, so **every flag change needs a deploy** (`cd infra && npm run deploy`, Docker Desktop running). Pushing to `main` is not enough.
- **The GitHub token is visible to all three functions.** It lives in SSM. The existing `loadSecrets` in `cmd/auto-lambda` loads **every** `/automations/*` parameter into the environment of **every** function at cold start, so the digest and deepdive functions also receive the GitHub token. Only the knowledge factory reads it. Keep the token scoped to the one repo for that reason.
- **`z_share` starts null.** It stays null for every ticker until at least 10 days with data exist in the 14 days before a day, so expect about two weeks of nulls after step 5.

## 1. Create the private data repo

1. On GitHub, create a **private** repository `sl6117/valhalla-knowledge`.
2. Tick "Add a README" so the `main` branch exists from the start.
3. Give the desk's bot read access to it.

- **Confirm:** the repo page shows `main` with one commit.
- **Undo:** delete or archive the repo. Nothing else depends on it until step 6.

## 2. Create a fine-grained token for that one repo

1. Go to GitHub → Settings → Developer settings → Fine-grained personal access tokens → Generate new token.
2. **Resource owner:** `sl6117`.
3. **Repository access:** "Only select repositories" → `valhalla-knowledge`, and nothing else.
4. **Permissions:** Repository permissions → **Contents: Read and write**. Metadata read-only is added automatically. Grant nothing else.
5. **Expiration:** pick one (90 days to a year), and put a reminder in your calendar a week before it.

When the token expires, publishing fails loudly: the run errors and a Telegram alert arrives. Sealing is unaffected. Generate a new token, then redo step 3 with the new value.

- **Confirm:** the token's page lists exactly one repository and the one permission.
- **Undo:** revoke the token on the same page.

## 3. Store the token in SSM (console only)

1. In the AWS console, open Systems Manager → Parameter Store → Create parameter.
2. **Name:** `/automations/KF_GITHUB_TOKEN`.
3. **Tier:** Standard. **Type:** **SecureString**, with the default `aws/ssm` key, the same as the other `/automations/*` parameters.
4. **Value:** paste the token. Never paste it anywhere else, including chats, files and commits.

- **Confirm:** the parameter is listed with type SecureString. Do not use "Show" in a shared screen.
- **Needs deploy:** no. Functions read SSM at cold start, so a running container keeps the old value until it is recycled. A deploy forces every function to pick up the new value.
- **Undo:** delete the parameter. Publishing (if on) then fails with "KF_GITHUB_TOKEN is not set".

## 4. Deploy to create the corpus table and the new function

1. Run `cd infra && npm run diff` and read it.
2. Expect only additions:
   - the `automations-corpus` table;
   - the `automations-knowledge-factory` function and its role;
   - its 09:30 schedule;
   - `CORPUS_TABLE` on the digest.
   The digest and deepdive functions must show no other change.
3. Run `cd infra && npm run deploy`.

- **Confirm:**
  - Run `npm run diff` again; it should show nothing pending.
  - In the console: DynamoDB lists `automations-corpus`; Lambda lists `automations-knowledge-factory`; EventBridge Scheduler lists the 09:30 schedule.
  - Optionally, test-invoke the new function with `{"project": "knowledge-factory", "dryRun": true}`. It logs "nothing fetched on <today>" because the corpus is still empty, and writes nothing.
  - The next 09:30 run logs the same line and exits successfully.
- **Undo:** in EventBridge Scheduler, disable the knowledge-factory schedule. This takes effect immediately, with no deploy. Removing the function means reverting the stack change and deploying. The corpus table is retained on purpose even if removed from the stack, so delete it by hand only if you mean to lose its history.

## 5. Turn on the digest's corpus flag

1. In `projects/twitter-digest/config.json`, set `"corpus": {"enabled": true}`.
2. Commit, then deploy.

- **Needs deploy:** yes.
- **Confirm:**
  - The next 09:00 digest log in CloudWatch has `[twitter-digest] corpus: stored N posts`.
  - The 09:30 knowledge-factory log has `<today>: sealed r1 (mentions …, entity_day …, posts_fetched N)` with the same N.
  - The digest itself must look exactly as before: same delivery and the same subscriber messages. The corpus write is fail-open, so a corpus failure logs `corpus write failed` and the digest still ships.
- **Undo:** set the flag back to `false` and deploy. The posts already stored stay in the table.

## 6. Turn on publishing

1. In `projects/knowledge-factory/config.json`, set `"publish": {"enabled": true, …}`. Leave `repo`, `branch` and `pathPrefix` as they are unless the repo moved. Leave `includeMentions` at `false`: the desk reads only `entity_day`, and the per-post `mentions` file (handles and post ids) stays sealed in the corpus table. Set it to `true` only if the desk asks for mentions. It then applies to revisions published from that point on.
2. Commit, then deploy.

- **Needs deploy:** yes.
- **Confirm:**
  - After the next 09:30 run, the repo has two commits per revision, named like `desk-feed: <date> r1 entity_day` and `desk-feed: <date> r1 manifest`.
  - The files are under `desk-feed/v1/entity_day/` and `desk-feed/v1/manifest/`. There is no `mentions/` directory, even though each manifest lists a mentions file; that is expected.
  - The log lists each `published desk-feed/v1/…` path.
  - A second run on the same day publishes nothing new unless a late fetch produced `r2`.
- **If publishing fails:** the run is recorded as failed and a Telegram alert arrives. The message never contains the token. The sealed files are safe in the corpus table, and the next run publishes whatever is missing from the last 3 days.
  - A message saying a file "already exists … with different content" means someone edited a published file in the repo. Investigate before doing anything: the job refuses to overwrite it, by design.
- **Undo:** set `enabled` back to `false` and deploy. Files already in the repo stay there; they are sealed and never change.

## Turning everything off

| What | How | Deploy needed |
|---|---|---|
| Publishing | `publish.enabled: false` | yes |
| Sealing | disable the knowledge-factory schedule in EventBridge Scheduler | no |
| Collecting posts | `corpus.enabled: false` in the digest config | yes |
| Token | revoke it on GitHub and delete the SSM parameter | no; it takes effect at the next cold start, and a deploy forces it |
