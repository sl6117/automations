# twitter-digest (project #1)

A 9am briefing from a curated X/Twitter List: fetch -> filter (no LLM) -> summarize (LLM) ->
deliver -> log.

## Files
- `config.json` — topics, engagement thresholds, account weights, noise keywords.
- `filter.js` — scoring, noise removal, near-duplicate dedup, incremental cursor. No tokens.
- `prompts/digest.md` — the LLM prompt (topic buckets, verifiability flags, source URLs).
- `summarize.js` — OpenRouter call (default Haiku) + an offline heuristic dry-run path.
- `pipeline.js` — orchestrates the stages, including "no content = no push".
- `references/account-weighting.md` — how to tune account weights (docs, not sent to model).
- `state.json` — incremental cursor (gitignored, created on first run).

## Run
```bash
npm run digest:mock     # offline: mock data, dry-run summary, console output
npm run digest          # real: uses .env (FETCH_SOURCE, OPENROUTER_API_KEY, Telegram, X_LIST_ID)
npm run cost:report     # show logged LLM spend
```

## Tuning
- Too noisy? raise `filters.minLikes` / `minImpressions`, or add `noiseKeywords`.
- A loud account dominates? lower its `accountWeights` entry.
- Serious-but-quiet account never shows? raise its weight (e.g. a central bank).
- Want different buckets? edit `config.json` `topics` (the prompt uses them verbatim).

## Switching the data source
Set `FETCH_SOURCE` in `.env`: `mock` (offline), `bird` (real, see `docs/setup/bird.md`), or
`xquik` (future, official API). The rest of the pipeline does not change.
