#!/usr/bin/env bash
# Entry point used by the scheduler (launched) to run the digest once.
# launched provides a minimal environment (no direnv, no Homebrew PATH),
# so this script loads .env itself and runs the prebuilt Go binary (bin/auto)
# Rebuild after code changes: go build -o bin/auto ./cmd/auto

set -euo pipefail

# Derive the repo root from this script's location (no hardcoded path).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO"

if [ ! -x bin/auto ]; then
    echo "ERROR: bin/auto missing. Build it: go build -o bin/auto ./cmd/auto" >&2
    exit 1
fi

# Load secrets/config from .env (simple KEY=value lines).
set -a
# shellcheck disable=SC1091
[ -f .env ] && . ./.env
set +a

export AUTOMATION_ROOT="$REPO"
mkdir -p logs

{
    echo "===== digest run: $(date -u +%Y-%m-%dT%H:%M:%SZ) ====="
    ok=0
    for attempt in 1 2 3 4 5 6; do
        if ./bin/auto run twitter-digest; then
            ok=1
            break
        else
            code=$?
            if [ "$code" -eq 3 ]; then
                echo "non-retryable failure (exit $code); not retrying"
                break
            fi
        fi
        echo "attempt $attempt failed; retrying in 600s..."
        sleep 600
    done
    [ "$ok" = "1" ]
    echo "===== end run ====="
} >> logs/launchd-digest.log 2>&1

