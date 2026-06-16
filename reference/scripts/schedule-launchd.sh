#!/usr/bin/env bash
# Installs/loads the 9am daily digest as a macOS launchd LaunchAgent.
# The plist is GENERATED from the current repo location, so this works no matter where the
# project lives (as long as it is NOT under ~/Desktop, ~/Documents, or ~/Downloads, which
# macOS TCC blocks background jobs from reading).
# Rollback: ./scripts/unschedule-launchd.sh

set -euo pipefail

LABEL="com.thomaslee.twitter-digest"
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
        <string>${REPO}/scripts/run-digest.sh</string>
    </array>
    <key>StartCalendarInterval</key>
    <dict>
        <key>Hour</key><integer>9</integer>
        <key>Minute</key><integer>0</integer>
    </dict>
    <key>RunAtLoad</key>
    <false/>
    <key>StandardOutPath</key>
    <string>${REPO}/logs/launchd-stdout.log</string>
    <key>StandardErrorPath</key>
    <string>${REPO}/logs/launchd-stderr.log</string>
</dict>
</plist>
EOF
echo "Wrote $DEST (repo: $REPO)"

# Reload cleanly (ignore errors if not currently loaded).
launchctl bootout "gui/$(id -u)/${LABEL}" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$DEST"
launchctl enable "gui/$(id -u)/${LABEL}"

echo "Loaded ${LABEL}. It will run daily at 09:00."
echo "Test it now:  launchctl kickstart -k gui/$(id -u)/${LABEL}"
echo "Then check:   tail -f \"$REPO/logs/launchd-digest.log\""
