# Account weighting (reference — loaded on demand, not sent to the model)

This explains the `accountWeights` map in `config.json`. It is documentation for you/the agent;
the pipeline applies it in code (`filter.js`), so it does NOT consume tokens at digest time.

## How it works
Final rank = `engagementScore(tweet) * accountWeight(handle)`.

- `engagementScore` = log10 of a weighted blend (replies/RTs/quotes count more than likes,
  impressions count a little). Log dampening stops one viral tweet from dominating.
- `accountWeight` multiplies that by how serious/credible the account is.

## Setting weights (handle keys are lowercase)
- `default`: 1.0 — every account not listed.
- 1.5–2.0: primary sources and high-credibility outlets (central banks, major papers,
  recognized domain experts).
- 1.2–1.5: solid secondary sources / reputable commentators.
- < 1.0: accounts you follow but distrust for accuracy (engagement farmers, hype accounts) —
  down-weight instead of removing.

## Tuning tips
- If your digest is dominated by one loud account, lower its weight or raise `minLikes`.
- If serious-but-quiet accounts never appear, raise their weight (e.g. a central bank that gets
  few likes but is authoritative).
- Noise keywords in `config.json` hard-drop spam regardless of weight.
