# Jev defect cascade (roadmap #6)

**Status:** approved 2026-10-21. Not wired. Thresholds stay off until a read-through.
**Depends on:** the per-bullet defect shadow in `bc8a058` (local `main`, one commit ahead of `origin/main` at the time of this decision). Production still runs the 2026-10-11 image until `cd infra && npm run deploy`. Pushing to `main` does not deploy.

## Official name (what to say in interviews)

**Host-routed model cascade with localized repair.**

Jev returns P(defect) on atomic questions. Go reads the question id and picks one repair. Sonnet remains the adopt gate. A high probability means the defect is present.

Close cousins:

| Name | Relation to this arc |
|------|----------------------|
| Model cascade / selective prediction | Cheap model triages. The expensive model takes the uncertain band. |
| Generator–critic / Reflexion | Today's `runReviseLoop`. This arc keeps it and narrows the critique to the failing bullets. |
| Action-space design | Digit and citation checks in Go, beside the URL check already in `eval.go`. A missing digit is a failed edit, not a prompt plea. |
| Critic-triggered replan | Bite 5. The Sunday brief uses the same router. A spine claim with no source opens replan. A prose defect opens revise. |
| Disagreement sampling | Owner reading time goes to artifacts where Jev and Sonnet conflict. |

## What is already true

`jevshadow.go` asks one defect per bullet (`bN_fact`, `bN_cite`, `bN_attr`, `bN_section`, `bN_merged`), one `split` question, and one `tN_missing` question per source tweet. High means the defect is present. Rollups are the max in each dimension: faithfulness = fact/cite/attr, topicRouting = section, coverage = missing, clarity = split/merged. A dimension with no questions rolls up to 0.

The first draft is always scored (`initialJev` / `initialJevDetail`). The sent text is scored again only when revise changes it (`jev` / `jevDetail`). One call when the text is unchanged, and both fields get that call. `jevScale` is `defect`. Older artifacts omit it and their four numbers are P(pass). Do not compare the two scales.

Sonnet still judges. `runReviseLoop` revises only when Sonnet faithfulness fails, one attempt, and ships the revision only when the re-judge says faithfulness passes. Otherwise the original draft ships. Judge, revise, and Jev errors do not block delivery. Model pin stays `jev-1.13.0`.

The October 12–21 shadow was four whole-digest P(pass) scores in a narrow band. That band is what a compound question produces. A noul near 0.5 means yes and no are equally likely. Atomic questions are meant to sit near 0 or 1. If the first defect-scale artifacts still sit near 0.5, the questions are still compound and the router waits.

## The route

Rollups pick the band. The question id picks the repair. Proposed cuts, still hypotheses: send when every rollup is ≤ 0.2, repair when any repairable question is ≥ 0.8, escalate otherwise.

```
Haiku draft
    → Jev, one call
         ├─ every rollup ≤ 0.2     → send band
         ├─ a repairable question ≥ 0.8 → one repair
         └─ anything else, or a Jev error → escalate
    → Sonnet adopt gate
    → deliver
```

| Question id | Rollup | Repair | Adopt when |
|-------------|--------|--------|------------|
| `bN_fact`, `bN_cite`, `bN_attr` | faithfulness | One rewrite of those bullets, shown with their cited tweets only | Those probabilities fall, and Sonnet faithfulness passes |
| `bN_merged`, `split` | clarity | Split a joined bullet, or join the split story | Clarity questions fall, and faithfulness does not rise |
| `bN_section` | topicRouting | Move the bullet under a legal topic, including Other | That section question falls |
| `tN_missing` | coverage | Add at most one bullet for that tweet | The new bullet stays under 0.8 on fact, cite, and attr |

Several faithfulness hits share one revise call. The revision that lowers one defect and raises another does not ship. Coverage stays off until `tN_missing` questions name real omissions. The 2026-07-13 read-through found coverage fails were often invented requirements.

Skipping Sonnet on the send band waits for a read-through of days Jev called clean. The July calibration lesson was that passes are weaker evidence than fails.

## Weekly deepdive

Same host skill, different questions. Digest bullet questions do not transfer onto the brief.

- **Before the synthesizer.** Score each researcher report: does it assert a fact that none of its fetched pages state? A high score marks that report unusable for a corroborated claim. The synthesizer still sees it and must hedge.
- **After the editor.** A spine claim with no source opens the replan edge from `2026-08-08-deepdive-critic-triggered-replan.md`. A side claim gets one sentence rewritten. Hedge and structure problems skip research and go to text revise.

## Bite order

Delivery changes only where the bite says live. Each bite is owner-typed, then tested, then committed. Deploy is a separate explicit step.

| Bite | What changes | Delivery |
|------|----------------|----------|
| 0. Magnitude guard | Parse each number plus a closed unit list (`억` `만` `조`, million/billion, млрд/млн, and attached K/M/B) into a plain count. A bullet's count must sit within 10% of a count in a cited kept tweet. `1.5억` and `$150 million` match. `$1.5 billion` does not. URLs are stripped first. Recorded as an `eval.go` failure. | Unchanged. Eval stays fail-open. |
| 1. Shadow route | Store `jevRoute`: `send`, `repair:<ids>`, or `escalate`, computed from `initialJevDetail`. | Unchanged. Compare the route to Sonnet. |
| 2. Faithfulness repair | The critique lists bullet ids at or above 0.8 and includes those tweets only. | Live, inside the existing `reviseBudget`. Sonnet still always runs. |
| 3. Clarity and section | Separate operators for split, merged, and section. | Live. Coverage adds stay off. |
| 4. Skip Sonnet on the send band | Only after a read-through of days where every rollup was ≤ 0.2. | Live after that read-through. |
| 5. Deepdive gates | Report scores before synth. Brief scores choose replan or revise. | Live on the Sunday run after the digest route is dull. |

Bite 0 exists because translation is supposed to change the digits. `1.5억` is 150,000,000, which an English bullet should write as `$150 million`. The broken translation `$1.5 billion` keeps the digits `1.5` and is off by 10×. Matching raw digit tokens would accept the broken line and reject the correct one. Month names written as numbers (`October` versus `10월`) are outside this bite. A false failure there is a log line. Delivery still proceeds.

Later, still on this arc, after the route is dull:

- **Disagreement sampling.** Owner attention goes to artifacts where Jev and Sonnet conflict. That set is the escalate band.
- **Cross-lingual consistency.** After both drafts exist, one question: does the Korean digest assert a fact the English one does not? Revise the language that added the fact.

## Leave until the route is dull

- **Embeddings.** The archive is a week of sections. Lexical retrieve already feeds researchers, and archive text stays a hint.
- **Best-of-N drafts.** Defects are local. A second full digest costs a full generation. One bullet rewrite uses `reviseBudget`.
- **Fine-tuning Haiku on repairs.** One digest a day is a small labeled set. Replay yesterday's drafts through a changed prompt first, the way `cmd/rejudge` replays verdicts.
- **A model that picks the next stage.** The replan decision already parks the open supervisor after budgets, contracts, and fail-open delivery are boring.

## Portfolio one-liner

A cheap defect model routes the day. Go picks one localized repair. Sonnet still decides whether the repair ships.
