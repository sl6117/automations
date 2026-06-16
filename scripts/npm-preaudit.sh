#!/usr/bin/env bash
# Pre-download security audit for an npm package.
# Run this BEFORE `npm install <pkg>` to vet a new dependency.
#
# Usage: ./scripts/npm-preaudit.sh <package>[@version]
# Example: ./scripts/npm-preaudit.sh openclaw@latest
#
# What it does (all read-only, NEVER runs package install scripts):
#   1. Shows registry metadata (version, publish time, maintainers, repo).
#   2. Flags packages published very recently (< MIN_AGE_DAYS) — the window where
#      supply-chain compromises typically hide before being caught.
#   3. Resolves the FULL dependency tree with --ignore-scripts and runs `npm audit`
#      (covers known CVEs and npm malware advisories).
#
# You review the output and decide. Nothing is installed.

set -euo pipefail

PKG="${1:-}"
MIN_AGE_DAYS="${MIN_AGE_DAYS:-3}"

if [ -z "$PKG" ]; then
  echo "Usage: $0 <package>[@version]" >&2
  exit 2
fi

echo "==================================================================="
echo " npm pre-download audit: $PKG"
echo "==================================================================="

echo
echo "--- Registry metadata ---"
npm view "$PKG" version time.modified maintainers repository.url homepage dist.integrity 2>&1 || {
  echo "Could not fetch metadata. Check the package name." >&2
  exit 1
}

echo
echo "--- Publish freshness check (min age: ${MIN_AGE_DAYS}d) ---"
modified="$(npm view "$PKG" time.modified 2>/dev/null | tr -d "'\"" || true)"
if [ -n "$modified" ]; then
  mod_epoch="$(date -j -f "%Y-%m-%dT%H:%M:%S" "${modified%%.*}" +%s 2>/dev/null || echo 0)"
  now_epoch="$(date +%s)"
  if [ "$mod_epoch" -gt 0 ]; then
    age_days=$(( (now_epoch - mod_epoch) / 86400 ))
    echo "Latest publish: $modified (~${age_days} days ago)"
    if [ "$age_days" -lt "$MIN_AGE_DAYS" ]; then
      echo "WARNING: published < ${MIN_AGE_DAYS} days ago. Consider waiting or pinning an older, vetted version."
    fi
  fi
fi

echo
echo "--- Full dependency tree audit (no scripts executed) ---"
workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT
(
  cd "$workdir"
  npm init -y >/dev/null 2>&1
  npm install "$PKG" --package-lock-only --ignore-scripts --no-audit --no-fund >/dev/null 2>&1
  npm audit 2>&1 || true
)

echo
echo "Audit complete. Review above before running: npm install $PKG"
