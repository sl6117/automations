# Style (translated from `~/.cursor/rules/personal-style.mdc` and the pre-push skill)

- Comments are functional: why the code is this way, for the dev joining in six
  months. No refactoring history, no banners, no restating the code.
- Decisions and analysis go in `docs/decisions/`, not in code comments.
- Respect existing decisions before overriding them. Incremental over big-bang.
- Conventional commits (`feat:`, `fix:`, `chore:`, `docs:`), subject under 72 chars.

## Go + DynamoDB checks before calling work done
- `go build ./... && go vet ./... && go test ./...` all exit 0.
- Timestamps written to DynamoDB are `time.Now().UTC()`.
- DynamoDB reserved words (`status`, `name`, `type`, `key`, `value`, ...) go through
  `ExpressionAttributeNames`.
- `UpdateItem` when only some fields change, so `createdAt` and friends survive.
- Tests inject their store explicitly. Never rely on env-selected storage in a test:
  it has hit the real table before.
