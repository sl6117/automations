// Token + cost logging. Every real LLM run appends one JSON line to logs/cost-log.jsonl,
// so you can see exactly what your automations cost over time (`npm run cost:report`).

import { appendFileSync, mkdirSync } from 'node:fs';
import { join } from 'node:path';

// Prices in USD per 1,000,000 tokens (input, output). Update as provider pricing changes.
// Source: provider pricing as of 2026-04. Keep model keys lowercase.
export const PRICES = {
  'anthropic/claude-haiku-4.5': { in: 1.0, out: 5.0 },
  'anthropic/claude-sonnet-4.6': { in: 3.0, out: 15.0 },
  'anthropic/claude-opus-4.6': { in: 5.0, out: 25.0 },
};

export function estimateCost(model, inputTokens, outputTokens) {
  const p = PRICES[String(model).toLowerCase()];
  if (!p) return 0;
  return (inputTokens / 1e6) * p.in + (outputTokens / 1e6) * p.out;
}

function logDir() {
  const root = process.env.AUTOMATION_ROOT || join(process.cwd());
  return join(root, 'logs');
}

export function logRun({ project, model, usage, itemCount, deliveredTo, dryRun }) {
  const costUsd = dryRun ? 0 : estimateCost(model, usage.inputTokens, usage.outputTokens);
  const record = {
    ts: new Date().toISOString(),
    project,
    model,
    dryRun: Boolean(dryRun),
    inputTokens: usage.inputTokens || 0,
    outputTokens: usage.outputTokens || 0,
    totalTokens: usage.totalTokens || 0,
    costUsd: Number(costUsd.toFixed(6)),
    itemCount,
    deliveredTo,
  };
  const dir = logDir();
  mkdirSync(dir, { recursive: true });
  appendFileSync(join(dir, 'cost-log.jsonl'), JSON.stringify(record) + '\n');
  return record;
}
