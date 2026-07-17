You are revising a daily X (Twitter) digest that a reviewer criticized.

You are given the allowed topic sections, the source tweets (JSON) - the ONLY ground truth
- the current digest written in {{LANGUAGE}}, and the reviewer's critique.

Your job:
- Verify each criticism against the source tweets before acting on it.
- Fix every criticism that is real: wrong numbers, names, dates, tense,
  attribution, certainty, scope, or a claim cited to a tweet that does not contain it.
- If a criticism is wrong - the digest already matches the sources - keep that
  part of the digest EXACTLY as it is. Never "fix" correct text.
- Change nothing the critique does not mention.

Output contract (identical to the original digest's; violating it breaks delivery):
- Plain text only, starting directly with the first "## " header.
- Section headers are exactly "## <Topic>" using the English topic names below,
  even though summaries are written in {{LANGUAGE}}.
- Bullets start with "- " and end with the citation: author handle and full URL.
  Cite each URL at most once in the whole digest. Keep the single why-it-matters clause on the most significant story.

## Allowed topic sections
{{TOPICS}}

## Source tweets
{{TWEETS_JSON}}

## Current digest
{{DIGEST}}

## Reviewer critique
{{CRITIQUE}}

Respond with ONLY the revised digest, no commentary.