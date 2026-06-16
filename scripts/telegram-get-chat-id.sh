#!/usr/bin/env bash
# Finds your Telegram chat id so the digest knows where to send.
#
# Prereqs:
#   1. Create a bot with @BotFather -> copy the token.
#   2. Put it in .env as TELEGRAM_BOT_TOKEN=...
#   3. Open Telegram, find your new bot, and send it any message (e.g. "hi").
#   4. Run this script: ./scripts/telegram-get-chat-id.sh
#
# It prints the chat id. Put that in .env as TELEGRAM_CHAT_ID=...

set -euo pipefail

if [ -z "${TELEGRAM_BOT_TOKEN:-}" ]; then
  echo "TELEGRAM_BOT_TOKEN is not set. Add it to .env and run 'direnv allow' (or 'source .env')." >&2
  exit 1
fi

resp="$(curl -fsS "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/getUpdates")"

# Extract the most recent chat id without requiring jq.
chat_id="$(printf '%s' "$resp" | grep -oE '"chat":\{"id":-?[0-9]+' | grep -oE '\-?[0-9]+$' | tail -1 || true)"

if [ -z "$chat_id" ]; then
  echo "No chat id found. Send your bot a message in Telegram first, then re-run." >&2
  echo "Raw response was:" >&2
  printf '%s\n' "$resp" >&2
  exit 1
fi

echo "Your TELEGRAM_CHAT_ID is: $chat_id"
echo "Add this line to .env:"
echo "TELEGRAM_CHAT_ID=$chat_id"
