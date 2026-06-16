# npm download policy & audit log

Two safeguards against npm supply-chain attacks on this machine.

## 1. Pre-download audit (do this BEFORE installing anything new)

```bash
./scripts/npm-preaudit.sh <package>[@version]
```

It is fully read-only and never runs the package's install scripts. It reports:
- registry metadata (version, publish time, maintainers, repo, integrity),
- a freshness warning if the package was published < 3 days ago (the window where
  compromises hide before detection — tune with `MIN_AGE_DAYS`),
- a full dependency-tree `npm audit` (known CVEs + npm malware advisories).

You review the output, then decide whether to install. Rule of thumb:
- Stop if audit shows a **malware** advisory or a **critical** in a package that runs scripts.
- For brand-new versions of critical tooling, prefer pinning a slightly older, vetted version.

## 2. Device-wide download log

Every `npm install`-family command and every `npx` run in an interactive zsh shell is recorded.

- Log file: `~/Library/Logs/npm-downloads.log`
- Logger: `~/.npm-audit/npm-logger.zsh` (sourced from `~/.zshrc`)
- Format: `ISO8601 <TAB> user= <TAB> tool= <TAB> cwd= <TAB> node= <TAB> npm= <TAB> cmd=`

View recent installs:
```bash
tail -20 ~/Library/Logs/npm-downloads.log
```

### Coverage & limitations
Catches: install/i/add/ci/update/upgrade (npm) and all npx, in interactive zsh, in any repo.
Does NOT catch: other shells (bash), non-interactive scripts/CI, calls via absolute path,
or other user accounts. For stronger coverage, add a PATH-shim `npm` binary (optional hardening).

### Rollback
Remove `source "$HOME/.npm-audit/npm-logger.zsh"` from `~/.zshrc` and delete `~/.npm-audit/`.

## Hardening ideas (optional, later)
- `npm config set audit-level moderate` to surface audit issues by default.
- Consider `--ignore-scripts` for installs that don't need native builds.
- Re-run `scripts/npm-preaudit.sh` after major dependency upgrades.
