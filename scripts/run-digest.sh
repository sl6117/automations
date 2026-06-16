#!/usr/bin/env bash
# Entry point used by the scheduler (launchd) to run the digest once.
# Loads Node 22 (via nvm) and .env, then runs the pipeline, appending output to a log.

set -euo pipefail

# Derive the repo root from this script's location, so the job keeps working if the
# project is moved (no hardcoded path).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO"

# Make Node 22 available even in launchd's minimal environment.
export NVM_DIR="$HOME/.nvm"
# shellcheck disable=SC1091
[ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"
nvm use 22 >/dev/null 2>&1 || true

# Load secrets/config from .env (simple KEY=value lines).
set -a
# shellcheck disable=SC1091
[ -f .env ] && . ./.env
set +a

export AUTOMATION_ROOT="$REPO"
mkdir -p logs

{
  echo "===== digest run: $(date -u +%Y-%m-%dT%H:%M:%SZ) ====="
  npm run --silent digest
  echo "===== end run ====="
} >> logs/launchd-digest.log 2>&1
