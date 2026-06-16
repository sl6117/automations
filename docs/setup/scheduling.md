# Setup: scheduling the 9am digest

The project must live OUTSIDE `~/Desktop`, `~/Documents`, `~/Downloads` (macOS TCC blocks
background jobs from reading those). Recommended home: `~/automations/twitter_summary`.

## Primary: launchd (native macOS, reliable while the Mac is awake)

Install / load:
```bash
./scripts/schedule-launchd.sh
```
This generates `~/Library/LaunchAgents/com.thomaslee.twitter-digest.plist` from the repo's
actual location and loads it. It runs daily at 09:00 local time.

Test immediately (don't wait for 9am):
```bash
launchctl kickstart -k "gui/$(id -u)/com.thomaslee.twitter-digest"
tail -f logs/launchd-digest.log
```

Remove:
```bash
./scripts/unschedule-launchd.sh
```

Notes:
- The job runs `scripts/run-digest.sh`, which loads Node 22 (nvm) and `.env`, then `npm run digest`.
- It will show in System Settings > General > Login Items as a `bash` background item
  ("unidentified developer"). That is expected and is the legitimate scheduler.
- If the Mac is asleep at 9:00, launchd runs the job at the next wake.

## Alternative: OpenClaw cron (once the agent is onboarded)

If you've completed `openclaw onboard`, you can let the agent run it instead:
```bash
openclaw cron add --name "Twitter digest" --cron "0 9 * * *" --tz "America/Los_Angeles" \
  --session isolated \
  --message "Run the shell command: cd ~/automations/twitter_summary && npm run digest" \
  --announce --channel telegram
```
- `--session isolated` keeps it out of your main chat history (cheaper, cleaner).
- This requires the OpenClaw gateway/daemon to be running.

## Which to use
Start with launchd (works without OpenClaw onboarding, fewer moving parts). Move to OpenClaw cron
if you want the agent to manage it and report into your chat. On a VPS later, use system cron or
a systemd timer instead of launchd.
