// Sends the digest to Telegram via the Bot API. Needs TELEGRAM_BOT_TOKEN + TELEGRAM_CHAT_ID
// (see docs/setup/credentials.md). Splits messages over Telegram's 4096-char limit.

const TG_LIMIT = 4000; // a little under 4096 for safety

export function createTelegramDeliverer() {
  const token = process.env.TELEGRAM_BOT_TOKEN;
  const chatId = process.env.TELEGRAM_CHAT_ID;

  return {
    name: 'telegram',
    configured() { return Boolean(token && chatId); },
    async deliver(text) {
      if (!token || !chatId) {
        throw new Error(
          'Telegram not configured. Set TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID in .env ' +
          '(see docs/setup/credentials.md).',
        );
      }
      const chunks = splitMessage(text, TG_LIMIT);
      for (const chunk of chunks) {
        const res = await fetch(`https://api.telegram.org/bot${token}/sendMessage`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            chat_id: chatId,
            text: chunk,
            disable_web_page_preview: true,
          }),
        });
        if (!res.ok) {
          const body = await res.text().catch(() => '');
          throw new Error(`Telegram ${res.status}: ${body.slice(0, 300)}`);
        }
      }
      return { ok: true, channel: 'telegram', parts: chunks.length };
    },
  };
}

function splitMessage(text, limit) {
  if (text.length <= limit) return [text];
  const parts = [];
  let buf = '';
  for (const line of text.split('\n')) {
    if ((buf + '\n' + line).length > limit) {
      if (buf) parts.push(buf);
      buf = line;
    } else {
      buf = buf ? buf + '\n' + line : line;
    }
  }
  if (buf) parts.push(buf);
  return parts;
}
