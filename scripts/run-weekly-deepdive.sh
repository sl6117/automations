#!/usr/bin/env bash
# Entry point used by launchd to run the weekly deep-dive once.
# launchd provides a minimal environment (no direnv, no Homebrew PATH),
# so this script loads .env itself and runs the prebuilt Go binaries.
# Rebuild after code changes: go build -o bin/auto ./cmd/auto && go build -o bin/digest-mcp ./cmd/digest-mcp

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO"

if [ ! -x bin/auto ] || [ ! -x bin/digest-mcp ]; then
    echo "ERROR: bin/auto or bin/digest-mcp missing. Build: go build -o bin/auto ./cmd/auto && go build -o bin/digest-mcp ./cmd/digest-mcp" >&2
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
    echo "===== weekly-deepdive run: $(date -u +%Y-%m-%dT%H:%M:%SZ) ====="
    ok=0
    # only 2 attempts: each retry is a full multi-agent run with real LLM spend
    for attempt in 1 2; do
        if ./bin/auto run weekly-deepdive; then
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
} >> logs/launchd-weekly-deepdive.log 2>&1