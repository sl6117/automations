#!/usr/bin/env bash
# Removes the Sunday weekly deep-dive launchd job.

set -euo pipefail

LABEL="com.thomaslee.weekly-deepdive"
DEST="$HOME/Library/LaunchAgents/${LABEL}.plist"

launchctl bootout "gui/$(id -u)/${LABEL}" 2>/dev/null || true
rm -f "$DEST"
echo "Removed ${LABEL} and $DEST"