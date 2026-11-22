# Safety rules (translated from the owner's Cursor rules)

Source: `~/.cursor/rules/{credential-hygiene,git-index-safety,no-auto-deletion,no-aws-cli,security-vigilance}.mdc`.
`verify-before-done` and `replan-on-divergence` already live in `~/.claude/CLAUDE.md`.
The Bash guard in `.claude/hooks/guard-bash.py` enforces the mechanical parts.

## Credentials
- Never output, log, echo, or commit API keys, tokens, passwords, chat ids, webhook
  URLs, or AWS account ids. Listing variable *names* is fine; values are not.
- Never create files named `*credentials*`, `*secret*`, `*token*`, `*.pem`, `*.key`.
- If a hardcoded secret turns up in source, flag that it exists without copying it.

## AWS
- Never run the `aws` CLI and never read `~/.aws/*`. Infra changes go through CDK.
- If AWS resource data is needed, ask the owner to run the command and share output.
- Local testing uses DynamoDB Local with dummy credentials, never the real account.

## Git index
- No `git add -A`, `git add .`, `git rm -r .`. Stage specific paths.
- More than ~50 staged paths is suspicious: stop and confirm.

## Deletion
- Confirm before deleting any source file, even one created this session. Name the
  path and say how to undo it. Temp dirs and build output are exempt.

## Dependencies and supply chain
- Name every new dependency before adding it. Prefer the standard library, then
  first-party vendor SDKs. See `docs/security/npm-download-policy.md` for npm.
- Never `curl | sh`, never run obfuscated commands, never touch `~/.ssh`, `~/.aws`,
  `~/.gitconfig`, `~/.npmrc`.

## Deploy
- Pushing to `main` does not deploy, and `cd infra && npm run deploy` does. Neither
  happens without the owner asking for it in that turn.
