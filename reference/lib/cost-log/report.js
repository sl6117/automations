// Prints a summary of logged LLM costs. Run with: npm run cost:report

import { readFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';

function logPath() {
  const root = process.env.AUTOMATION_ROOT || process.cwd();
  return join(root, 'logs', 'cost-log.jsonl');
}

function main() {
  const path = logPath();
  if (!existsSync(path)) {
    console.log('No cost log yet at', path);
    return;
  }
  const lines = readFileSync(path, 'utf8').split('\n').filter(Boolean);
  const runs = lines.map((l) => JSON.parse(l));

  const byMonth = new Map();
  let totalCost = 0;
  let totalTokens = 0;

  for (const r of runs) {
    const month = r.ts.slice(0, 7);
    const agg = byMonth.get(month) || { runs: 0, cost: 0, tokens: 0 };
    agg.runs++;
    agg.cost += r.costUsd || 0;
    agg.tokens += r.totalTokens || 0;
    byMonth.set(month, agg);
    totalCost += r.costUsd || 0;
    totalTokens += r.totalTokens || 0;
  }

  console.log('LLM cost report');
  console.log('================');
  console.log(`Runs: ${runs.length}   Tokens: ${totalTokens.toLocaleString()}   Cost: $${totalCost.toFixed(4)}`);
  console.log('');
  console.log('By month:');
  for (const [month, agg] of [...byMonth.entries()].sort()) {
    console.log(`  ${month}: ${agg.runs} runs, ${agg.tokens.toLocaleString()} tokens, $${agg.cost.toFixed(4)}`);
  }
  const lastReal = runs.filter((r) => !r.dryRun).at(-1);
  if (lastReal) {
    console.log('');
    console.log(`Last real run: ${lastReal.ts} (${lastReal.model}) $${lastReal.costUsd.toFixed(6)} for ${lastReal.itemCount} items`);
  }
}

main();
