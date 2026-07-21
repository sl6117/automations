# Step 8 design: weekly deep-dive (orchestration + MCP)

**Date:** 2026-07-20
**Status:** design agreed with owner; build starting. Read this before touching
`cmd/digest-mcp` or `projects/weekly-deepdive`.

## The shape

Weekly pipeline: read the week's digest artifacts -> pick the biggest story -> fan out
researcher agents that fetch linked/external sources (absorbs the source-corroboration
idea) -> synthesize a long-form brief -> editor gate -> deliver via existing sinks.

Two new pieces:

1. **`cmd/digest-mcp`** — an MCP server over stdio exposing the digest archive as
   READ-ONLY tools, all backed by the existing `storage.Store` seam:
   `list_runs`, `get_artifact`, `get_verdicts`, `get_cost`, plus `fetch_url`
   (the one write-nothing tool that touches the open internet — see constraints below).
   Read-only is a design property, not a limitation: the server is safe to hand to any
   client. Mutating tools (e.g. queue requeue) deliberately live elsewhere.
2. **`projects/weekly-deepdive`** — a normal `runner.Project`. It is itself an MCP
   *host*: it embeds the Anthropic API plus an MCP client connection, and bridges the
   model's tool calls to the server. Same loop Cursor runs under the hood.

The same server serves two clients: the orchestrator's agents, and the owner
interactively via Cursor. Tools built for one pipeline become infrastructure — this
M+N property (any client, any server) is the point of MCP; evaluated one integration
at a time it never looks worth the ceremony.

## Roles and contracts (each role = one LLM call with a typed JSON contract)

- **Planner**: reads the week's runs via MCP tools; outputs
  `{story, whyChosen, sourceTweetIDs, researchQuestions[]}`.
- **Researchers** (fan-out, one per question): fetch linked/external sources; output
  `{question, findings[], sources[], corroborated: bool}`. Uncertainty is VALID output —
  a researcher cannot "fail" by being unsure.
- **Synthesizer**: writes the brief. Every asserted claim must trace to a
  `corroborated: true` finding; anything else must carry an explicit hedge label
  ("reported but not corroborated"). It may not launder an uncorroborated claim into
  an assertion.
- **Editor**: Sonnet judges the brief against the researchers' findings (the ground
  truth for this pipeline, exactly as slimTweets is ground truth for the daily digest).
  Gates on high-precision fails only (step-6 lesson: passes are weak evidence).

## Decisions (owner, 2026-07-20 design session)

1. **Build both sides of the protocol** — our own server AND the host loop, not just
   consuming existing servers. The learning goal is step 8's reason to exist.
2. **Hedge, don't drop, never fail the run on corroboration gaps.** Fail-open with
   visible uncertainty: `corroborated: false` usually means "couldn't check" (paywall,
   timeout, no external link), not "false" — failing the run would conflate
   infrastructure flakiness with content problems; dropping silently loses information
   with no trace. Same philosophy as the revise loop (never errors, draft ships with
   its verdict) and the single-source-labeling shelf idea. Run-level failure is
   reserved for real breakage: API errors, malformed role output after retries.
3. **`fetch_url` is our own tool on digest-mcp**, not a third-party fetch server —
   keeps corroboration auditable and teaches tool design. Constraints: timeout, response
   size cap, no credentials or env vars in outbound requests, GET only.
4. **Dependency**: `github.com/modelcontextprotocol/go-sdk` pinned at stable v1.6.1
   (official SDK, maintained with Google). The MCP spec revs 2026-07-28; upgrading is
   a version bump later, irrelevant for stdio use.

## The reusable pattern: verification between agents

This is the lesson step 8 exists to teach; keep it in mind when reviewing or extending
any multi-agent code in this repo.

Hedging vs. failing is not a style preference — it relocates where the hard gate lives.
"Fail the whole run on an uncorroborated claim" is fail-closed: maximum precision,
bought by letting one paywalled article kill the entire brief. Hedging is fail-open
with the uncertainty made legible: the pipeline always ships, quality degrades
gracefully, and the reader sees the system's honest confidence in each claim.

That choice makes the gate *checkable*. "Is this claim actually true?" is not a
question a judge can answer reliably (step-6 calibration: judge passes are weak
evidence). "Did the synthesizer respect the labels — does every assertion trace to a
corroborated finding, does every uncorroborated claim carry its hedge?" IS checkable,
mechanically or by a judge, at high precision.

So the pattern: **agents pass structured claims with confidence attached, and the gate
verifies the structure was respected — contract compliance, not truth.** Verification
between agents means checking that each role honored its contract with the next, not
re-litigating the content. This is the calibrated-gate idea from steps 6-7 lifted from
around one generator to between agents, and it generalizes beyond this project.

## Bite sequence

1. Server skeleton with `list_runs` — smallest thing that speaks the protocol end to
   end (includes speaking JSON-RPC to it by hand for the learning value).
2. Plug into Cursor; query the real archive from chat (first client, the payoff moment).
3. Remaining read tools: `get_artifact`, `get_verdicts`, `get_cost`.
4. Host side in Go: MCP client + Anthropic tool-use bridge, proven with a trivial
   "answer requires calling list_runs" test.
5. Planner role, then researchers + `fetch_url`, synthesizer, editor, delivery.
   Weekly scheduling (launchd) is the LAST bite; `--dry-run` covers development.

Bites 1-3 are useful standing alone even if the project stalled there.

## Gotchas specific to this work

- An MCP stdio server's stdout IS the protocol channel. Never print to stdout;
  diagnostics go to stderr (Go's `log` default).
- The server reads the REAL archive via `storage.FromEnv` — fine because tools are
  read-only. Tests still inject `&storage.FS{Root: t.TempDir()}` per the ambient-env
  rule.
- `fetch_url` output enters researcher prompts: treat fetched pages as untrusted input
  (prompt-injection surface). The researcher contract's structured output is the
  containment: findings must quote sources, and the synthesizer trusts labels, not prose.
