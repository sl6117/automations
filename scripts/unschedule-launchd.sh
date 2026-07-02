#!/usr/bin/env bash
# Removes the 9am daily digest launchd job.

set -euo pipefail

LABEL="com.thomaslee.twitter-digest"
DEST="$HOME/Library/LaunchAgents/${LABEL}.plist"

launchctl bootout "gui/$(id -u)/${LABEL}" 2>/dev/null || true
rm -f "$DEST"
echo "Removed ${LABEL} and $DEST"
