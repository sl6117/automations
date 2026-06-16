# Setup: credentials & interactive steps

These are the steps that need YOUR accounts/logins. Everything else (code, pipeline, scheduling
scaffolding) is already built. Do these once, then the digest runs hands-free.

## 1. OpenRouter API key (the LLM)
1. Go to https://openrouter.ai/keys and create a key.
2. Put it in `.env`:
   ```
   OPENROUTER_API_KEY=sk-or-...
   DIGEST_MODEL=anthropic/claude-haiku-4.5
   ```
3. OpenRouter is pay-as-you-go; add a few dollars of credit. A daily digest costs ~$1-3/month on Haiku.

> Note: the standalone pipeline calls OpenRouter directly. OpenClaw (below) is the optional
> agent/chat + scheduler layer. You can run the digest with just the OpenRouter key.

## 2. Telegram bot (delivery)
1. In Telegram, message **@BotFather** -> `/newbot` -> follow prompts -> copy the bot **token**.
2. Put it in `.env`: `TELEGRAM_BOT_TOKEN=123456:ABC...`
3. Open your new bot in Telegram and send it any message (e.g. "hi").
4. Run: `./scripts/telegram-get-chat-id.sh` -> copy the printed id into `.env` as `TELEGRAM_CHAT_ID=`.
5. Set `DELIVER_TO=telegram` in `.env`.

## 3. OpenClaw onboarding (optional but recommended — the "agent harness")
OpenClaw is the always-on agent + scheduler. The CLI is already installed (`openclaw --version`).
Run the interactive wizard yourself (it stores credentials locally, so it can't be fully scripted):

```bash
openclaw onboard
```

In the wizard:
- Flow: **QuickStart** is fine to start.
- Gateway: local (loopback) default.
- Model provider: choose **OpenRouter** (or Anthropic) and paste your key. Set default model to
  `anthropic/claude-haiku-4.5` to keep costs low.
- Channel: add **Telegram** and paste the same bot token (lets you chat with the agent directly).
- Daemon: yes (installs a macOS LaunchAgent so it runs in the background).

Verify:
```bash
openclaw doctor
openclaw gateway status
```
Then message your bot something like "hello" and confirm the agent replies.

## 4. bird (X/Twitter data) — see docs/setup/bird.md
