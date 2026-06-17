# twitter-digest

## What
A daily digest of a curated X/Twitter list: fetch recent posts, filter noise in plain code (engagement + dedup),
summarize the best into topic buckets
- (AI/ Econ / Crypto/ Stocks/ World News /...)
- Some topics I might send to other friends or family
- Some topics will be private for me

## Success Criteria
- `auto run twitter-digest --dry-run` runs fully offline (mock data, no network, no tokens) and prints a topic-bucketed digest to the console.
- Filtering (engageent threshold + dedup) happens before any LLM call
- Every summarized item keeps its source URL (verifiability).


## LifeCycle stages used
- gather:  pkg/sources mock Source -> []Tweet
- process: filter (min engagement) + dedup     (plain Go, no tokens)
- reason:  summarize into topic buckets        (dry-run heuristic now; LLM later)
- act:     pkg/sinks console                    (telegram or something else)
- verify:  project_test.go
- log:     cost/run log                         (later)


## Config (config.json)
- minEngagement: drop posts below this (likes + reposts) threshold
- topics:        bucket names, e.g. ["AI", "Econ", "Crypto"]
- source:        which Source to use ("mock" now; "bird"/"xquik" later)
- model:         LLM model id (used once the reason step calls an LLM)


## Sources (swappable, one interface)
- mock (now): canned sample posts, 0 creds/tokens used
- bird (later): cookie-based X CLI
- xquik (later): official API
Swapped via config without changing the digest logic.


