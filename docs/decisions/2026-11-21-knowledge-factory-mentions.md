# Knowledge-factory ticker mentions and entity-day (M3)

**Status:** built on branch `kf/03-mentions`. Pure functions in `internal/kf`, not wired to anything yet. M4 wraps them in the sealed export. There is no runtime change and nothing to deploy.

## Universe

`projects/knowledge-factory/universe.json` is an allowlist of about 100 large US-listed equities and the major index and sector ETFs. A symbol that is not in it is never emitted, which is how "equities and ETFs only" is enforced: `$BTC` falls out because it is not listed. Seeded conservatively, so a missing ticker is preferred to a wrong one. The owner grows it from the manifest's unmapped-cashtag tally and bumps `version`.

The loader rejects:
- tickers that are not 1–5 upper-case letters with an optional `.X` class;
- duplicate tickers;
- a type other than `equity` or `etf`;
- aliases on ETFs;
- aliases shorter than four characters;
- an alias listed for two tickers;
- an alias that contains another ticker's alias as a phrase.

Aliases are limited to names that are distinctive in running text. Ordinary words (Apple, Target, Visa), people and other entities (Tesla, Ford, Disney) and folk names (Robinhood) are left out. Consumer brands (Netflix, Walmart, Airbnb) are kept: the entity is unambiguous, even though many mentions are about the product rather than the stock. Alias matches are always counted separately from cashtags so a consumer can ignore them.

## Matching (`ExtractorVersion = kf.extract.v1`)

Matching runs over `OwnText` and each `Ref.Text` separately, never over `Text`, which is the flattened concatenation and would count a quote twice.

- **Cashtag:** `$` and 1–5 ASCII letters, case-insensitive. The `$` must not follow a letter or digit, and the letters must not be followed by one. That rules out `$100`, `US$5`, `A$EXMPL` and `$EXMPLX1`.
  - A `.X` class suffix closed by a non-word character is matched whole (`$BRK.B`).
  - If only the base symbol is listed, the suffix is read as punctuation (`$EXMPL.A great quarter` is `EXMPL`). This favours recall.
  - An unlisted suffixed symbol is tallied with its suffix.
- **Alias:** case-sensitive, whole phrase, blocked by a Unicode letter or digit on either side. A leading `@` also blocks the match, because `@Name` is a handle, not the company in prose. A leading `#` does not.
- **Voices:** a voice is the author for own words and the referenced post's author for a ref. It falls back to the author id, then to the referenced post id, so three retweets of one post are three posts and one voice.

## Entity-day baseline

The baseline covers the 14 calendar days before `D` (fixed), counting only days with at least one run record. A data day with no mention of the ticker counts as share 0; a day with no run record is absent, not zero. The standard deviation is the sample one (n−1). `z_share` is null below 10 days or when the series is flat. A flat series is detected exactly, so rounding noise cannot produce a huge z. Each day is loaded on its own (`kf.Day`), so a later re-fetch of a post cannot move it out of an earlier day. Backfilled posts count nowhere.
