# hello

## What
The simplest automation: prints a greeting. Exists to exercise the Project
contract (Name + Run), dry-run handling, and registry wiring end-to-end.

## Success criteria
- `auto run hello` prints a greeting to stdout and exits 0.
- `auto run hello --dry-run` prints what it would do, performs no side effects, exits 0.

## Config
- `config.json` holds a `greeting` knob. NOTE: not wired into Runtime yet;
  config loading is a later milestone (internal/config). Present now to establish the per-project convention.