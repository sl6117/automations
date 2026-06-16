# Personal Automation Foundation

A small, reusable "automation OS" for your machine. You add automations as self-contained
projects; the shared plumbing (data fetching, filtering, delivery, cost logging) is reused
across all of them.

**Project #1:** a 9am Twitter/X digest that pulls a curated list, filters noise in plain code,
summarizes the rest with a cheap LLM into topic buckets (AI / econ / crypto / ...), keeps source
links for verifiability, and delivers to Telegram.

## Layout
```
.
├── AGENTS.md                 # always-loaded context for AI agents (short)
├── .env.example              # copy to .env and fill in (gitignored)
├── .envrc                    # direnv: auto-loads .env when you cd here
├── lib/
│   ├── fetch/                # data-source interface + adapters (bird, mock)
│   ├── deliver/              # telegram, console
│   └── cost-log/             # token/cost logging + report
├── projects/
│   └── twitter-digest/       # project #1
│       ├── config.json
│       ├── prompts/digest.md
│       ├── references/
│       ├── pipeline.js
│       └── state.json        # gitignored, created at first run
├── docs/decisions/           # architecture/decision notes
└── logs/                     # run + cost logs (gitignored)
```

## Quick start
1. `nvm use 22` (Node 22+ required).
2. `cp .env.example .env` and fill in values (or leave defaults for the offline demo).
3. Allow direnv once: `direnv allow`.
4. Offline demo (no keys, no tokens, no network):

```bash
npm run digest:mock
```

5. Real run: set `FETCH_SOURCE=bird`, `DIGEST_DRY_RUN=0`, `DELIVER_TO=telegram` in `.env`, then:

```bash
npm run digest
```

## Design principles
- **Filter before the model.** Engagement + dedup happen in code, so few tokens hit the LLM.
- **Cheapest capable model.** Default Claude Haiku; escalate only when a task needs it.
- **Context on demand.** `AGENTS.md` stays small; `references/` is loaded only when relevant.
- **Swappable sources.** `bird` now, Xquik later, by changing one env var.
- **Portable.** Runs locally; lift-and-shift to a VPS later for guaranteed scheduling.

See `docs/decisions/` for the reasoning behind these choices.
