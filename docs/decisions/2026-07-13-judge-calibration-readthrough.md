# Judge calibration read-through: results and rubric decisions

**Date:** 2026-07-13
**Status:** decided — rubric/config rewrite is the next bite; model escalation deferred pending A/B
**Context:** closes the analysis phase of roadmap step 6 (tier-2 LLM judge / evaluation engineering)

## What we did

Read every stored judge verdict with a human (the owner) and scored each failed dimension
agree / disagree / mixed against the owner's actual editorial standards. Dataset: 15 artifacts
(Jul 9–13, both languages, live + replayed), of which 13 carried verdicts and 2 carried judge
errors. 30 failed verdicts were scored. Passes were spot-checked opportunistically (one
confirmed false negative). Evidence was read via the new `cmd/showrun` viewer (built this
session; read-only by design — List/Get only, no write path).

Prerequisite cleanup: the 6 test-pollution artifacts from the Jul 8 ambient-env incident were
deleted from DynamoDB before scoring, so none of the results below are contaminated.

## Results by dimension (30 failed verdicts)

| Dimension    | Agree | Disagree | Mixed |
|--------------|-------|----------|-------|
| faithfulness | 7     | 3        | –     |
| topicRouting | 3     | 6        | –     |
| coverage     | 2     | 4        | –     |
| clarity      | 2     | 2        | 1     |

Plus: 1 confirmed false negative (faithfulness pass on Jul 11 EN despite a future-tense →
completed-fact error the Korean judge caught in the same run), and 2 judge errors
(Jul 12 EN orphan: API timeout; Jul 9 18:20 EN: the known backtick-after-JSON parse failure,
~8% rate, both retryable with `rejudge -force`).

### The headline finding

**The judge's perception is good; its rulebook is miscalibrated.** Nearly every agree is a
genuine fact mutation or unambiguous misfile. Nearly every disagree traces to a rule we never
wrote down (severity thresholds, omission criteria) or a rule we wrote wrong (the Politics
description) or a rule the judge was never told (the Other section, the why-it-matters clause).
That means most of the win is available for free, in prompt text.

### What the judge reliably catches (keep and trust)

- 10x money-translation error (1.5억 vs $1.5B), model name swap (GPT→Codex), future tense
  presented as completed fact (Starlink), scope inflation (benchmark-scoped claim made
  unqualified), certainty inflation ("says it will" → "commits"), added interpretation not in
  sources, miscitation (20% figure cited to a tweet that doesn't contain it — the judge even
  distinguished official posts from unverified retweets).
- Unambiguous topic misfiles (Bangkok restaurant fire under Science — caught independently by
  both language judges).
- Real writing-rule violations (one story split across two bullets; a garbled
  argument-by-analogy bullet).

**Faithfulness is the step-7 gate candidate: high precision (its catches are real), imperfect
recall (one confirmed miss). Trust its fails more than its passes.**

### Failure patterns (each with multiple specimens)

1. **Leniency instructions ignored (4x, coverage).** The rubric says "omitting minor tweets is
   fine"; the judge repeatedly wrote "while minor … should be included" — contradicting the
   rubric and itself in one sentence. Vibes-based leniency doesn't work on a small model;
   concrete decision rules are needed.
2. **Invented requirements.** "Should be included or explicitly justified as redundant" appears
   nowhere in the rubric. Fix is a closed-world rule, not a "don't hallucinate" plea.
3. **Verdict/reason self-contradiction (2x, topicRouting, both Jul 13).** The Reason concludes
   every placement is "correct" or "defensible", then outputs pass:false.
4. **Truncation penalties.** The judge fails digests because the *source tweet* is cut off,
   even when the digest only claims what the visible text supports (Jul 12 EN faithfulness,
   Jul 9 16:01 clarity). Truncated long tweets are a pipeline gap, not a digest fault.
5. **Dimension bleed / reason bundling.** Redundancy complaints inside faithfulness, routing
   complaints inside clarity and coverage; multiple half-complaints bundled per Reason
   (worst case: Jul 9 16:01 EN coverage failed with zero omission complaints, two of them
   self-negating). Matters for step 7, where Reason strings feed a revision prompt.
6. **Korean comprehension misreads (2x, same artifact).** Judge claimed the digest omitted
   "restitution" (피해배상금 is right there) and omitted "about" (약 is right there). These are
   capability failures, not rubric failures — the key input to the model-escalation decision.
7. **Inter-judge inconsistency.** Same config, opposite rulings on Charlie-Kirk-story-in-Politics
   (17:55 KO vs 18:20 KO); coverage failed in EN and passed in KO on identical omissions
   (Jul 13). Underspecified rules produce coin-flip verdicts.

### Generator/judge contract gaps (discovered during scoring)

The generator and judge share one view of the tweets by design, but NOT one view of the rules:

- **Other section:** `digest.md` sanctions a final "Other" section for fits-nothing stories;
  the judge only receives the nine config topics and has repeatedly dinged correct Other
  placements (FIFA, UFC-in-Other).
- **Omission permission:** `digest.md` never says the generator may drop junk; the judge rubric
  says omitting minor tweets is fine. The two prompts disagree about the same behavior.
- **Why-it-matters clause:** `digest.md` line 20 *instructs* one significance clause on the
  day's top story; the judge penalizes it as "editorial commentary". Ruling: ONE sanctioned
  why-it-matters clause is a feature; unmarked interpretation anywhere else is a violation
  (the Jul 10 KO ceasefire commentary stays a legitimate fail under this rule).

### Routing note

Delivery routes `##` sections to subscribers by topic name. Anything landing in Other reaches
no subscriber — which is why the owner never saw the UFC stories on Telegram and why the
calibration had to be done from artifacts, not delivered messages. Stories the owner actually
wants must be routed by fixing topic descriptions, not by widening Other.

## Decisions

### D1. Rewrite the judge rubric (`prompts/judge.md`) — next bite, owner types

Agreed changes (exact wording drafted separately):
- **Materiality threshold for faithfulness:** paraphrase is fine; fail only when facts change —
  tense, numbers, names, attribution, certainty, scope. (Owner's standard, near-verbatim.)
- **Defensible = pass for topicRouting:** fail only when a placement is clearly wrong per the
  descriptions; if placement is defensible, pass.
- **Concrete coverage rule:** a tweet that fits no topic section may always be omitted; junk
  (memes, bare links, content the pipeline can't see) may always be omitted; substantive
  content from curated list authors counts as significant.
- **Tell the judge the full generator contract:** Other section exists and is correct for
  fits-nothing stories; one why-it-matters clause on the top story is sanctioned.
- **Truncation rule:** a claim supported by the visible portion of a truncated tweet is
  faithful; do not penalize the digest for source truncation.
- **Closed-world rule:** fail a dimension only for violations of rules written in this prompt;
  anything not covered is not a failure.
- **Reason discipline:** each dimension's reason cites only issues belonging to that dimension.
- Output contract (pinned JSON skeleton) unchanged.

### D2. Fix the Politics topic description (`config.json`) — same bite

"About personal incidents of politicians" does not match the owner's intent and caused most
routing disputes (UFC plot, JD Vance deal, Kirk story coin-flips). Politics should cover
domestic political news broadly: politicians' statements/deals/scandals, political violence
and incidents. Owner also leans toward filing internationally-significant items in World News.
Also add omission permission and (already present) Other rules consistently to `digest.md`.

### D3. Measure before escalating the judge model

Sequence: rubric rewrite → `rejudge -force` A/B on a handful of already-scored artifacts →
compare verdicts against this session's human scores. If rulebook-class failures disappear but
capability-class failures persist (self-contradictions, dimension bleed, Korean misreads),
escalate the judge to a Sonnet-class model and re-A/B. Judge cost is small (~2.7k in / ~1k out
tokens per language per day), so escalation is affordable — but per house rules, we buy the
better model only when evidence demands it. Asymmetric generator/judge models (cheap drafter,
stronger evaluator) is an accepted pattern if it comes to that.

### D4. Shelf items surfaced by this session

- Fetch full text of truncated long tweets (X API note_tweets) — starves generator and judge.
- Video/linked-media understanding (Musk "Literally true" video) — post-roadmap.
- `cmd/showrun` kept as the artifact viewer; consider colorized output upgrades as needed
  (done: PASS/FAIL colors, indented reasons, separated tweets).

## What this taught (step 6 learning goals)

- An uncalibrated judge is just a second opinion; calibration = scoring the evaluator against
  a human, per dimension, then separating rubric bugs / config bugs / capability limits.
- Precision and recall of a judge are different properties: trust fails ≠ trust passes.
  Gate decisions (step 7) need the precision side.
- Generator and evaluator must share ONE contract; every place their prompts diverged produced
  systematic false failures.
- Concrete decision rules beat adverbs ("only clearly significant…") on small models.
- Same-model self-evaluation worked better than feared for fact-checking, but shows real
  comprehension limits in the non-English language.

## Next steps

1. Owner types D1 (judge.md) + D2 (config.json / digest.md) edits from drafted text.
2. `rejudge -force` A/B on selected artifacts (include the two judge-error artifacts).
3. Compare against this doc's human scores; decide on Sonnet judge (D3).
4. Update the north-star doc to close step 6; scope step 7 (draft → judge → revise loop,
   faithfulness as the leading gate candidate, advisory-only clarity).
