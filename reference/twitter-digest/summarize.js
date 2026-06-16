// Summarization stage. Two paths:
//   - real: call an LLM via OpenRouter (default model = Claude Haiku, cheap).
//   - dry-run: a local heuristic digest (no API, no tokens) for offline testing.
// Both return { digest, usage, model } so the pipeline treats them identically.

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const __dirname = dirname(fileURLToPath(import.meta.url));

function buildPrompt(tweets, config) {
  const template = readFileSync(join(__dirname, 'prompts', 'digest.md'), 'utf8');
  // Only send the fields the model needs — keeps the prompt (and cost) small.
  const slim = tweets.map((t) => ({
    author: t.author,
    handle: t.handle,
    url: t.url,
    text: t.text,
    score: Number(t.score?.toFixed?.(2) ?? 0),
  }));
  return template
    .replaceAll('{{DATE}}', new Date().toISOString().slice(0, 10))
    .replaceAll('{{TOPICS}}', config.topics.map((t) => `- ${t}`).join('\n'))
    .replaceAll('{{TWEETS_JSON}}', JSON.stringify(slim, null, 2));
}

export async function summarize(tweets, config, { dryRun = false } = {}) {
  const model = process.env.DIGEST_MODEL || 'anthropic/claude-haiku-4.5';

  if (dryRun || !process.env.OPENROUTER_API_KEY) {
    return { digest: heuristicDigest(tweets, config), usage: zeroUsage(), model: 'dry-run' };
  }

  const prompt = buildPrompt(tweets, config);
  const res = await fetch('https://openrouter.ai/api/v1/chat/completions', {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${process.env.OPENROUTER_API_KEY}`,
      'Content-Type': 'application/json',
      'HTTP-Referer': 'https://localhost/personal-automation-foundation',
      'X-Title': 'twitter-digest',
    },
    body: JSON.stringify({
      model,
      messages: [{ role: 'user', content: prompt }],
      temperature: 0.2,
      max_tokens: 900,
    }),
  });

  if (!res.ok) {
    const body = await res.text().catch(() => '');
    throw new Error(`OpenRouter ${res.status}: ${body.slice(0, 300)}`);
  }

  const data = await res.json();
  const digest = data.choices?.[0]?.message?.content?.trim() || '(empty response)';
  const u = data.usage || {};
  return {
    digest,
    usage: {
      inputTokens: u.prompt_tokens ?? 0,
      outputTokens: u.completion_tokens ?? 0,
      totalTokens: u.total_tokens ?? 0,
    },
    model,
  };
}

// --- offline heuristic (no tokens) ----------------------------------------

const TOPIC_KEYWORDS = {
  'AI & Tech': ['ai', 'model', 'gpu', 'openai', 'llm', 'data center', 'capex', 'benchmark', 'weights'],
  'Economics & Markets': ['inflation', 'cpi', 'fomc', 'rate', 'fed', 'tariff', 'trade', 'economist', 'earnings', 'market'],
  'Crypto': ['crypto', 'l2', 'rollup', 'etf', 'blob', 'eth', 'btc', 'coin', 'onchain'],
};

function classify(text, topics) {
  const lower = text.toLowerCase();
  for (const topic of topics) {
    const kws = TOPIC_KEYWORDS[topic] || [];
    if (kws.some((k) => lower.includes(k))) return topic;
  }
  return topics[topics.length - 1]; // "Other"
}

function heuristicDigest(tweets, config) {
  const buckets = new Map(config.topics.map((t) => [t, []]));
  for (const t of tweets) {
    const topic = classify(t.text, config.topics);
    buckets.get(topic).push(t);
  }
  const lines = [`Morning X digest (heuristic / dry-run) — ${new Date().toISOString().slice(0, 10)}`, ''];
  const accounts = new Set();
  let count = 0;
  for (const [topic, items] of buckets) {
    if (items.length === 0) continue;
    lines.push(`${topic}`);
    for (const t of items) {
      accounts.add(t.handle);
      count++;
      const oneLine = t.text.replace(/\s+/g, ' ').slice(0, 140);
      lines.push(`- ${oneLine} — @${t.handle} ${t.url}`);
    }
    lines.push('');
  }
  lines.push(`Sources: ${count} posts from ${accounts.size} accounts`);
  return lines.join('\n').trim();
}

function zeroUsage() {
  return { inputTokens: 0, outputTokens: 0, totalTokens: 0 };
}
