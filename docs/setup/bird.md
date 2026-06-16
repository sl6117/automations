# Setup: bird (X/Twitter data source)

`bird` is a fast X/Twitter CLI (`@steipete/bird`, same author as OpenClaw). It reads your
timeline/lists via GraphQL using your **existing X session cookies** — no password, no OAuth.

## Security note (read first)
- bird uses your X `auth_token` / `ct0` cookies. Treat these like a password — never commit them.
- Pre-download audit (already run 2026-06-10): `@steipete/bird@0.8.0`, trusted maintainer,
  0 vulnerabilities, 119 days old. Clean.
- Account risk: automated reads from your real account carry a small flagging risk. Keep it to
  once-daily (the cron) and consider an alt account if you want zero risk. The fetch layer is
  swappable to Xquik (official API) later via `FETCH_SOURCE=xquik` with no pipeline changes.

## Install (pick one)
Homebrew (prebuilt single binary — smallest supply-chain surface, recommended on macOS):
```bash
brew install steipete/tap/bird
```

Or npm (gets logged automatically by the download audit logger):
```bash
./scripts/npm-preaudit.sh @steipete/bird   # vet first
npm install -g @steipete/bird
```

Verify:
```bash
bird --version
```

## Authenticate
bird reads cookies in priority order: flags > env > auto-extracted browser cookies.

Easiest (browser cookies) — make sure you're logged into X in Chrome, then:
```bash
bird check        # shows which credential source is active
bird whoami       # confirms the logged-in account
```
For Arc/Brave: `bird whoami --chrome-profile-dir "/path/to/Arc/Profile"`.

Explicit cookies (more reliable for cron) — copy `auth_token` and `ct0` from your browser's
X cookies and put them in `.env` (gitignored):
```
AUTH_TOKEN=...
CT0=...
```
These are read by bird automatically. Tokens expire periodically — re-copy when `bird check` fails.

## Find your X List id
Use a curated List of serious accounts (better signal than the raw For You feed).
Open the list in your browser: `https://x.com/i/lists/<THIS_NUMBER>` -> put the number in
`.env` as `X_LIST_ID=`.

## Wire it to the pipeline
In `.env`:
```
FETCH_SOURCE=bird
X_LIST_ID=<your list id>
```
The bird adapter (`lib/fetch/bird.js`) runs one command, set by `BIRD_CMD`. The default is:
```
BIRD_CMD="bird list timeline {listId} --json --limit {limit}"
```
Confirm the exact list/timeline subcommand for your bird version with `bird --help`, and if it
differs, override `BIRD_CMD` in `.env` (use `{listId}` and `{limit}` placeholders). The adapter
parses either a JSON array or NDJSON and normalizes the fields, so only the command string
needs to match your version.

## Test
```bash
FETCH_SOURCE=bird node -e "import('./lib/fetch/index.js').then(async m => { const f = m.createFetcher('bird'); console.log((await f.fetchTweets({ listId: process.env.X_LIST_ID, limit: 5 })).length + ' tweets') })"
```
