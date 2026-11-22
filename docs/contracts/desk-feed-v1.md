# Desk feed v1: ticker attention from one curated X list

This is the contract for the daily files the knowledge factory publishes. It is written for a consumer who has never seen the producing repository. Everything you need to read the files is here, in the JSON Schemas beside this document, and in the synthetic fixtures under `fixtures/desk-feed-v1/`.

## What the feed is, and what it is not

The feed counts how often the posts in **one hand-curated X list of about 100 accounts** mention a US-listed equity or ETF on each UTC day. It also records the share of that day's posts that mention each ticker, and how that share compares with the ticker's own recent history.

It is **not** market-wide attention. In particular:
- **Counts are small.** A ticker with 3 posts is a busy day.
- **List changes move the numbers.** Adding or removing an account changes both counts and baselines, and nothing in the files marks list membership changes.
- **History starts when collection starts.** There is no data before the first fetched day, and baselines are short for the first weeks.
- **Coverage is limited to the universe.** Only tickers on a fixed allowlist (`universe_version` in each manifest) are ever reported. A ticker that is not on the list cannot appear, however often it is mentioned. The manifest's `unmapped_cashtags` shows the most common symbols that were seen but are not listed.
- **No sentiment.** The feed carries no confidence, sentiment or polarity value, and no trading signal. It is a measurement for research.

## Files

The producer seals one set of three files per **day** `D` (the UTC date on which posts were fetched, not the date they were written) and per **revision** `rN`:

| Path | Content |
|---|---|
| `mentions/<D>.r<N>.jsonl` | one `kf.mention.v1` record per line, sorted by `record_id` |
| `entity_day/<D>.r<N>.jsonl` | one `kf.entity_day.v1` record per line, sorted by `ticker` |
| `manifest/<D>.r<N>.json` | one `kf.manifest.v1` object |

- **Data files** are UTF-8, one compact JSON object per line, each line ending in `\n`. A day with no mentions has two empty data files (zero bytes) and a manifest.
- **Timestamps** are second-precision RFC3339 in UTC (`2026-11-21T16:00:01Z`). Fetch times are rounded **up** to the second, so a record is never available earlier than the moment it was actually fetched.

## What is delivered

Three files are sealed per revision, but only two are delivered to the desk's repo:

- **Delivered:** `entity_day/<D>.r<N>.jsonl` and `manifest/<D>.r<N>.json`.
- **Not delivered by default:** `mentions/<D>.r<N>.jsonl`. The producer retains it in its own store and can deliver it on request. It carries account handles and post ids, which the desk does not need for entity-day work.

The manifest is the seal of the whole revision, so the delivered manifest still lists the `mentions` file with its hash and record count. That entry is not an error, and its absence from the repo does not make the revision incomplete. A revision is complete when its manifest is present, because the manifest is always written last.

If mentions delivery is switched on later, it applies to revisions published from then on. Earlier revisions are not backfilled.

Schemas: `kf.mention.v1.schema.json`, `kf.entity_day.v1.schema.json`, `kf.manifest.v1.schema.json` (JSON Schema 2020-12). The schemas do not forbid extra properties, because v1 may gain fields.

### `kf.mention.v1`: one ticker mentioned in one post

*Retained by the producer and not delivered by default; available on request.*

There is at most one record per (post, ticker, kind, in).

| Field | Type | Meaning |
|---|---|---|
| `schema_version` | string | `"kf.mention.v1"` |
| `record_id` | string | lower-case hex `sha256("kf.mention.v1|<post_id>|<ticker>|<kind>|<in>")`. Stable across revisions, so the same mention keeps its id in `r2`. |
| `ticker` | string | upper case, always on the allowlist; a share class is written `BRK.B` |
| `kind` | `cashtag` \| `alias` | `cashtag`: the post wrote `$TICKER` (any case). `alias`: the post wrote the company's name, from a short curated list of unambiguous names. Alias matches are less precise; filter them out if you want cashtags only. |
| `in` | `own` \| `ref` | `own`: in the account's own words. `ref`: inside a post it retweeted, quoted or replied to. A retweet's content is always `ref`, because the account wrote none of it. |
| `post.id` | string | the X post id (a decimal string; numeric order is time order) |
| `post.url` | string | link to the post |
| `post.author_id` | string, optional | X user id of the account that posted it |
| `post.author_handle` | string | `@handle` of the account that posted it (the retweeter, for a retweet) |
| `post.created_at` | timestamp, optional | when the post was written. Omitted when X did not provide it. |
| `post.ref_type` | `retweeted` \| `quoted` \| `replied_to`, optional | the post's **first** reference. Omitted for a plain post. |
| `post.ref_post_id` | string, optional | id of that referenced post |
| `post.ref_author_handle` | string, optional | `@handle` of that referenced post's author, when known |
| `available_at` | timestamp | when we fetched the post: the earliest moment you could have known about this mention. **Use this, not `created_at`, for point-in-time work.** |
| `age_at_fetch_s` | integer, optional | `available_at − created_at` in seconds. Omitted when `created_at` is unknown. |
| `metrics` | object | one snapshot taken at `available_at`: `likes`, `reposts`, `replies`, `quotes`, `impressions`, `author_followers` (the posting account's followers) |
| `source_pack` | string | which set of accounts produced the post; `"core"` in v1 |

The `post.ref_*` fields describe the post's first reference even when the ticker was found in a later one. A post that both quotes and replies is rare, but in that case use `in` to see where the ticker was, and treat `ref_*` as context only.

### `kf.entity_day.v1`: one ticker on one day

There is one row per ticker with at least one mention that day.

| Field | Type | Meaning |
|---|---|---|
| `schema_version` | string | `"kf.entity_day.v1"` |
| `ticker` | string | as in mentions |
| `date` | `YYYY-MM-DD` | the UTC fetch day `D` |
| `posts` | integer | distinct posts mentioning the ticker (any kind, own or ref) |
| `posts_cashtag` | integer | of those, posts with a cashtag match |
| `posts_own` | integer | of those, posts whose own words mention it |
| `unique_voices` | integer | distinct voices. A voice is the posting account for `own` words and the referenced post's author for `ref`: three accounts retweeting one post is 3 posts and 1 voice. A quote where both sides mention the ticker contributes both accounts. |
| `posts_fetched` | integer | every post fetched that day: the denominator |
| `share` | number | `posts / posts_fetched` |
| `baseline.days` | integer | how many of the 14 calendar days before `D` had any fetch. Only those days count. |
| `baseline.mean_share` | number | mean of the ticker's daily share over those days, counting a fetched day without a mention as 0 |
| `baseline.std_share` | number | sample standard deviation (n−1) of the same series |
| `z_share` | number or null | `(share − mean_share) / std_share`. **null** when `baseline.days < 10` or `std_share == 0`. |
| `window_truncated` | boolean | some fetch that day hit its page cap, so posts are missing from the day |
| `window_partial` | boolean | some fetch that day stored fewer posts than it fetched (a storage failure), so posts are missing from the day |
| `available_at` | timestamp | the latest fetch time of the day |

The 14-day window and the 10-day minimum are fixed parts of v1. They are not tuning parameters.

### `kf.manifest.v1`: the seal of one revision

| Field | Type | Meaning |
|---|---|---|
| `schema_version` | string | `"kf.manifest.v1"` |
| `date` | `YYYY-MM-DD` | the day `D` |
| `revision` | integer | `N`, starting at 1 |
| `sealed_at` | timestamp | when this revision was sealed |
| `source_window.first_fetch`, `.last_fetch` | timestamps | the earliest and latest fetch that day |
| `files[]` | array | for each data file: `path` (relative, e.g. `mentions/2026-11-21.r1.jsonl`), `records` (line count), `sha256` (lower-case hex of the file's bytes) |
| `universe_version` | string | version of the ticker allowlist used |
| `extractor_version` | string | version of the matching rules used |
| `posts_fetched` | integer | posts fetched that day (also present when no ticker was mentioned) |
| `window_truncated`, `window_partial` | booleans | as in entity-day, for the day as a whole |
| `unmapped_cashtags[]` | array | at most 20 `{symbol, posts}`: well-formed cashtags not on the allowlist, by distinct posts, most frequent first |
| `supersedes` | integer or null | the previous revision of this day, or null for `r1` |

## Consumer rules

1. **Point in time:** use `available_at` (or the manifest's `sealed_at`) for point-in-time work, never `created_at`.
2. **Sealed revisions never change.** A revision exists once its manifest exists. Its files are never rewritten or deleted.
3. **Choosing a revision:** for a decision at time T, use the highest revision of the day whose `sealed_at` ≤ T. A later revision (a late fetch landed) supersedes earlier ones from its `sealed_at` onward.
4. **Verify** every **delivered** data file's sha256 and line count against its manifest entry before use. A manifest entry for a file that was not delivered (by default, `mentions`) is not an error.
5. **Ignore unknown fields.** v1 only ever adds fields. Removing or renaming a field, or changing a field's meaning, publishes a new `v2` path while v1 continues to be produced.
6. **Zero versus missing:**
   - On a sealed day with `window_truncated` and `window_partial` both false, a ticker with no entity-day row had **zero** mentions.
   - A day with no files, or one flagged truncated or partial, is **missing data, not zero**.
7. **`z_share` null means "not enough baseline"**, not zero and not "normal".
8. **Engagement metrics are a single snapshot** taken at fetch time, so they depend on how old the post was (`age_at_fetch_s`). Do not compare engagement across posts without accounting for age. For a retweet, X reports the original's repost count and zero likes.
9. **Baselines are fixed when a revision is sealed.** A later revision of an earlier day does not rewrite the already-sealed revisions of later days. Each revision's baseline reflects the corpus as it stood when that revision was sealed.

## Never in an export

The files never contain any of the following:
- post text, quoted text, or display names;
- subscriber names, chat ids or emails, or who follows which source;
- secrets;
- any confidence, sentiment or polarity value.

Account handles, ids and post URLs are public metadata and are included.

## Fixtures

`fixtures/desk-feed-v1/` holds one synthetic day (`2026-11-21`) in two revisions. `r1` is four posts fetched at 16:00 UTC. `r2` adds a late fetch at 20:00 UTC that hit the page cap. The fixtures include a retweet, a quote with mentions on both sides, an alias match, a class-share ticker, a post with unknown `created_at`, an unmapped cashtag, and both a numeric and a null `z_share`.

The fixtures are produced by the producer's own test, which seals the two revisions from synthetic posts and compares the bytes. To regenerate them after an intended change, run the following from the producer repository:

```
go test ./internal/kf -run TestDeskFeedFixtures -update
```
