# Critic-triggered replan for weekly-deepdive (roadmap #5)

**Status:** designing / building (2026-08-08)  
**Depends on:** parallel fan-out (`9e96ecf`), editor + revise loops already live

## Official name (what to say in interviews)

**Critic-triggered replan on a host-orchestrated multi-role DAG**
(also: conditional replan, plan-and-execute + replan, adaptive research expansion).

Close cousins in industry/practice:

| Name | Relation to this bite |
|------|------------------------|
| Host-orchestrated multi-role pipeline | Baseline you already ship (Go owns stages) |
| Generator–critic / Reflexion | Today’s **revise** loop (rewrite brief, same evidence) |
| Plan-and-execute + replan | Critic/executor says plan insufficient → host asks for new steps |
| Corrective / adaptive retrieval (CRAG-style) | “Evidence weak → fetch more” — here: new *research questions*, not chunks |
| LangGraph-style conditional edges | `editor.Pass?` → ship; else → replan branch (bounded) |

This is the **production-conservative** multi-agent extension: the model proposes
**additive** questions; Go still chooses when the branch runs, caps rounds/Qs,
fans out, synthesizes, and fail-open ships. It is **not** an open agent swarm
(no self-invented topology, no peer negotiation, no dropping prior work in v1).

## Before → after (technique change)

**Before (fixed DAG + text refine):**

```
plan → researchFanOut → synth → editor
                              └─ fail → revise loop (same reports) → deliver
```

Mid-run, only the **brief text** can change. Question set and evidence set are fixed
after the first research wave.

**After (v1):**

```
plan → researchFanOut → synth → editor
                              └─ fail → replan (0–N new Qs)
                                       ├─ empty → revise loop → deliver
                                       └─ Qs → fan-out → append reports
                                                → synth → editor → revise? → deliver
```

| Layer | Before | After v1 |
|-------|--------|----------|
| Who picks questions | Planner only | Planner, then **replan** may add |
| Evidence set | Fixed after Fan1 | Append-only growth |
| On editor fail | Rewrite prose | Prefer **new research**, then rewrite |
| Topology owner | Go | Go (conditional edge; model fills contract) |

**Two failure modes (why both loops):**

- Thin / missing corroboration → **replan** (need new evidence)
- Hedge/structure mistakes → **revise** (evidence OK, prose wrong)

Empty `newResearchQuestions` = replan saying “this is a revise problem.”

## v1 contract (locked)

- **When:** after first editor fail, **before** text revise
- **Role:** Haiku + forced `submit_replan`
- **In:** story, asked questions, reports, editor failures
- **Out:** `newResearchQuestions` (0–N) + `rationale`
- **Caps:** `replanBudget: 1`, `maxReplanQuestions: 2`; exact-string dedup vs asked
- **Run:** fan-out new Qs → append → synth → edit again
- **Out of scope for v1:** drop/cancel prior questions, mid-flight sharing, open topology

## Ways to go further (after v1 is proven)

Ordered from smallest → largest blast radius:

1. **Selective trigger** — replan only when failure text matches evidence gaps
   (corroboration / missing angle), else jump straight to revise (saves tokens).
2. **Budget > 1** — second replan round if still failing (hard cost ceiling).
3. **Drop / replace questions** — true replan of the research set, not append-only
   (harder eval: what to discard?).
4. **Shared scratchpad** — later researchers see earlier findings (coordination vs
   independence trade-off).
5. **Checkpoint / resume** — persist mid-DAG state on Dynamo (queue substrate exists).
6. **Open supervisor** — model chooses next *stage* (leave the fixed DAG). Only after
   contracts + cost meters + fail-open habits are boringly solid.

## Portfolio one-liner

> “I started with a deterministic multi-role research DAG, added parallel fan-out for
> latency, then a critic-triggered replan edge so the host can expand research when
> the editor shows evidence gaps — still host-owned topology, not a free swarm.”
