// bird adapter — reads a curated X List timeline via the `bird` CLI.
//
// IMPORTANT: bird's exact subcommands/flags can change between versions. The single
// invocation is isolated here (BIRD_CMD) so you can adjust it in one place. Confirm the
// command for your installed bird with `bird --help` (see docs/setup/bird.md).
//
// This adapter expects bird to emit JSON (an array of tweets, or NDJSON). It then maps
// whatever field names bird uses onto our normalized shape via normalizeTweet().

import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { normalizeTweet } from './index.js';

const execFileAsync = promisify(execFile);

// Override via env if your bird version differs, e.g.:
//   BIRD_CMD="bird list timeline {listId} --json --limit {limit}"
const DEFAULT_BIRD_CMD = 'bird list timeline {listId} --json --limit {limit}';

export function createBirdFetcher() {
  return {
    name: 'bird',
    async fetchTweets({ listId, sinceId = null, limit = 100 } = {}) {
      if (!listId) {
        throw new Error('bird adapter needs an X List id. Set X_LIST_ID in .env.');
      }

      const template = process.env.BIRD_CMD || DEFAULT_BIRD_CMD;
      const rendered = template
        .replaceAll('{listId}', String(listId))
        .replaceAll('{limit}', String(limit));
      const [bin, ...args] = rendered.split(/\s+/);

      let stdout;
      try {
        ({ stdout } = await execFileAsync(bin, args, { maxBuffer: 32 * 1024 * 1024 }));
      } catch (err) {
        if (err.code === 'ENOENT') {
          throw new Error(
            'bird is not installed or not on PATH. See docs/setup/bird.md.',
          );
        }
        throw new Error(`bird command failed: ${err.stderr || err.message}`);
      }

      const raw = parseBirdOutput(stdout);
      let rows = raw.map(normalizeTweet).filter((t) => t.id && t.text);
      if (sinceId) rows = rows.filter((t) => safeGt(t.id, sinceId));
      return rows.slice(0, limit);
    },
  };
}

function parseBirdOutput(stdout) {
  const text = (stdout || '').trim();
  if (!text) return [];
  // Try a single JSON value (array or object) first.
  try {
    const v = JSON.parse(text);
    if (Array.isArray(v)) return v;
    if (Array.isArray(v.tweets)) return v.tweets;
    if (Array.isArray(v.data)) return v.data;
    return [v];
  } catch {
    // Fall back to NDJSON (one JSON object per line).
    const rows = [];
    for (const line of text.split('\n')) {
      const t = line.trim();
      if (!t) continue;
      try { rows.push(JSON.parse(t)); } catch { /* skip non-JSON lines */ }
    }
    if (rows.length === 0) {
      throw new Error(
        'Could not parse bird output as JSON. Adjust BIRD_CMD to request JSON (see docs/setup/bird.md).',
      );
    }
    return rows;
  }
}

function safeGt(a, b) {
  try { return BigInt(a) > BigInt(b); } catch { return String(a) > String(b); }
}
