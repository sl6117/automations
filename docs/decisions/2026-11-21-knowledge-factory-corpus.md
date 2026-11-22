# Knowledge-factory corpus store (M2)

**Status:** built on branch `kf/02-corpus-store`, flag off. Nothing changes in production until the owner deploys (`cd infra && npm run deploy`) and then sets `"corpus": {"enabled": true}` in `projects/twitter-digest/config.json` and deploys again. Pushing to `main` does not deploy.

## What

`internal/corpus` stores every post the digest fetches, kept and dropped, as one compact JSON line (`corpus.post.v1`), plus one run record per fetch (`corpus.run.v1`). The digest writes both after `filter`, before the quiet-day early return.

## Why

- **Keep every fetched post.** The reads are the main cost and are already paid for. Today a dropped post keeps its metrics in `logs/sourcestats/` but loses its text, and a kept post survives only inside per-language artifacts. Extraction passes (tickers first, in M3) need the whole fetch, including what the digest filter rejected.
- **A second table (`automations-corpus`).** `storage.Dynamo.List` is a full-table Scan. Putting a few hundred posts a day into the main table would make every existing `List` (runs, sourcestats, citations) read the corpus too. The corpus is read only by computed key, never `List`.
- **Day partitions by fetch time, UTC.** `corpus/posts/<YYYY-MM-DD>.jsonl` holds what we knew by that day, which is what point-in-time exports need. A post's own `createdAt` is kept on the line. Every post in one fetch is stamped with the run's `fetchedAt`, so one fetch never straddles two partitions.
- **Chunked appends.** Lines are joined with newlines into appends of at most ~200 KB, half the 400 KB DynamoDB item limit. A 250-post day is two or three writes instead of 250. That also avoids depending on `Dynamo.Append`'s nanosecond-timestamp sort key never colliding between writes. `Dynamo.Get` and `FS.Get` already return a stream's appends joined with newlines, so a chunk reads back as ordinary lines.
- **Run records.** `written < fetched` marks a partially stored day, so an export can tell a failed write from a quiet day. `truncated` carries the page-cap flag.
- **Retries.** A retried run appends the same posts again. `Load` dedupes by id, keeping the latest `fetchedAt` (its metrics are the newer snapshot).
- **Fail-open.** As with `saveSourceStats`, a corpus failure is logged and the digest still ships. With the flag off the corpus store is never resolved. Dry runs write nothing.

## Privacy

Stored post text stays private in the corpus table. Exports (M4) carry ids and derived counts only. No subscriber data enters the corpus.
