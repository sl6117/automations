#!/usr/bin/env bash
# Installs/loads the Sunday 10:00 weekly deep-dive as a macOS launchd LaunchAgent.
# 10:00, not 9:00: the daily digest runs at 9:00 and the deep-dive reads its artifacts.
# The plist is GENERATED from the current repo location (must not live under
# ~/Desktop, ~/Documents, or ~/Downloads - macOS TCC blocks background jobs there).
# Rollback: ./scripts/unschedule-weekly-launchd.sh

set -euo pipefail

LABEL="com.thomaslee.weekly-deepdive"
REPO="$(cd "$(dirname "$0")/.." && pwd)"
DEST="$HOME/Library/LaunchAgents/${LABEL}.plist"

case "$REPO" in
  "$HOME"/Desktop/*|"$HOME"/Documents/*|"$HOME"/Downloads/*)
    echo "ERROR: $REPO is in a macOS TCC-protected folder."
    echo "Background jobs cannot read it. Move the project to e.g. ~/automations and retry."
    exit 1
    ;;
esac

mkdir -p "$HOME/Library/LaunchAgents" "$REPO/logs"

cat > "$DEST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>${LABEL}</string>
    <key>ProgramArguments</key>
    <array>
        <string>/bin/bash</string>
        <string>${REPO}/scripts/run-weekly-deepdive.sh</string>
    </array>
    <key>StartCalendarInterval</key>
    <dict>
        <key>Weekday</key><integer>0</integer>
        <key>Hour</key><integer>10</integer>
        <key>Minute</key><integer>0</integer>
    </dict>
    <key>RunAtLoad</key>
    <false/>
    <key>StandardOutPath</key>
    <string>${REPO}/logs/launchd-weekly-stdout.log</string>
    <key>StandardErrorPath</key>
    <string>${REPO}/logs/launchd-weekly-stderr.log</string>
</dict>
</plist>
EOF
echo "Wrote $DEST (repo: $REPO)"

# Reload cleanly (ignore errors if not currently loaded).
launchctl bootout "gui/$(id -u)/${LABEL}" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$DEST"
launchctl enable "gui/$(id -u)/${LABEL}"

echo "Loaded ${LABEL}. It will run Sundays at 10:00."
echo "Test it now:  launchctl kickstart -k gui/$(id -u)/${LABEL}"
echo "Then check:   tail -f \"$REPO/logs/launchd-weekly-deepdive.log\""