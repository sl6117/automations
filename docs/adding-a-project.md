# Capstone: adding project #2 (reuse the foundation)

The foundation is built so a new automation = copy the template, swap the fetch + prompt. The
filter/deliver/cost-log/scheduling plumbing is reused as-is.

## Example: "FB Marketplace alert" or "job listings alert"

### 1. Scaffold from the template
```bash
cp -R projects/twitter-digest projects/marketplace-alert
rm projects/marketplace-alert/state.json   # start with a fresh cursor
```

### 2. Add a fetch adapter (the only real new code)
Create `lib/fetch/marketplace.js` exporting `createMarketplaceFetcher()` that returns the
normalized shape from `lib/fetch/index.js` (`id, author, text, url, createdAt, metrics, ...`).
Register it in `lib/fetch/index.js`:
```js
case 'marketplace':
  return createMarketplaceFetcher();
```
For a listings source, map: `id`=listing id, `text`=title + description, `url`=listing url,
`metrics.likes`/etc.=0 (or price into a custom field). Keep the normalized contract.

### 3. Adjust config + prompt
- `projects/marketplace-alert/config.json`: replace `topics` with your criteria buckets
  (e.g. price ranges, locations), set thresholds, and use `noiseKeywords` to drop scams.
- `projects/marketplace-alert/prompts/digest.md`: rewrite for the new task
  (e.g. "list items matching <criteria>, flag likely scams, include the listing URL").

### 4. Reuse everything else
`filter.js` (scoring/dedup/cursor), `summarize.js`, `lib/deliver/*`, `lib/cost-log/*` work
unchanged. The pipeline pattern is the same; copy `pipeline.js` and adjust the `PROJECT` name and
the import of its local `filter.js`/`summarize.js`.

### 5. Schedule it
Duplicate `scripts/run-digest.sh` -> `scripts/run-marketplace.sh` (point it at the new pipeline),
and add a second launchd label in a copy of `scripts/schedule-launchd.sh` (e.g.
`com.thomaslee.marketplace-alert`) with whatever `StartCalendarInterval` you want. Or, once
OpenClaw is onboarded, schedule it with `openclaw cron` instead (see docs/setup/scheduling.md).

## The reusable pattern
fetch (swap) -> filter (reuse) -> summarize (swap prompt) -> deliver (reuse) -> log (reuse).
That separation is the whole point of the foundation: new automations are mostly configuration
and one adapter, not a new codebase.
