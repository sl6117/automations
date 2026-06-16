#!/usr/bin/env node
// Twitter digest pipeline: fetch -> filter (no LLM) -> summarize (LLM) -> deliver -> log.
// Run: npm run digest        (uses .env settings)
//      npm run digest:mock   (offline: mock data + dry-run summary + console output)

import { readFileSync, writeFileSync, existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

import { createFetcher } from '../../lib/fetch/index.js';
import { createDeliverer } from '../../lib/deliver/index.js';
import { logRun } from '../../lib/cost-log/index.js';
import { applyFilters } from './filter.js';
import { summarize } from './summarize.js';

const __dirname = dirname(fileURLToPath(import.meta.url));
const PROJECT = 'twitter-digest';
const STATE_PATH = join(__dirname, 'state.json');

function loadConfig() {
  return JSON.parse(readFileSync(join(__dirname, 'config.json'), 'utf8'));
}

function loadState() {
  if (!existsSync(STATE_PATH)) return { lastSeenId: null };
  try { return JSON.parse(readFileSync(STATE_PATH, 'utf8')); } catch { return { lastSeenId: null }; }
}

function saveState(state) {
  writeFileSync(STATE_PATH, JSON.stringify(state, null, 2) + '\n');
}

async function main() {
  const config = loadConfig();
  const source = process.env.FETCH_SOURCE || 'mock';
  const dryRun = process.env.DIGEST_DRY_RUN === '1';
  const deliverTo = process.env.DELIVER_TO || 'console';
  const listId = process.env.X_LIST_ID || null;

  const state = loadState();
  const log = (...a) => console.error('[digest]', ...a); // logs to stderr, keeps stdout clean

  log(`source=${source} dryRun=${dryRun} deliver=${deliverTo} sinceId=${state.lastSeenId ?? '(none)'}`);

  // 1. fetch
  const fetcher = createFetcher(source);
  const tweets = await fetcher.fetchTweets({ listId, sinceId: state.lastSeenId, limit: config.fetchLimit });
  log(`fetched ${tweets.length} tweets`);

  // 2. filter (no tokens)
  const { kept, newCursor, stats } = applyFilters(tweets, config, state.lastSeenId);
  log(`filter stats:`, JSON.stringify(stats));

  // 3. no content = no push
  if (kept.length === 0) {
    log('nothing new worth sending — skipping delivery.');
    saveState({ lastSeenId: newCursor, lastRunAt: new Date().toISOString() });
    return;
  }

  // 4. summarize
  const { digest, usage, model } = await summarize(kept, config, { dryRun });
  log(`summarized with model=${model} tokens=${usage.totalTokens}`);

  // 5. deliver
  const deliverer = createDeliverer(deliverTo);
  if (!deliverer.configured()) {
    log(`deliverer "${deliverTo}" not configured; falling back to console.`);
    await createDeliverer('console').deliver(digest);
  } else {
    const result = await deliverer.deliver(digest);
    log('delivered:', JSON.stringify(result));
  }

  // 6. cost/token log
  const record = logRun({
    project: PROJECT,
    model,
    usage,
    itemCount: kept.length,
    deliveredTo: deliverTo,
    dryRun,
  });
  log(`logged run: $${record.costUsd} for ${record.itemCount} items`);

  // 7. advance the incremental cursor
  saveState({ lastSeenId: newCursor, lastRunAt: new Date().toISOString() });
  log(`cursor advanced to ${newCursor}`);
}

main().catch((err) => {
  console.error('[digest] FAILED:', err.message);
  process.exit(1);
});
