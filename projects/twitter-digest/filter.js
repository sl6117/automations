// Pre-LLM filtering. Everything here runs in plain code — NO tokens spent.
// Goal: turn a noisy fetch of ~150 tweets into a small, high-signal, deduplicated set,
// ranked by (engagement x how serious the account is), so only the cream reaches the model.

// --- scoring ---------------------------------------------------------------

// Engagement score with diminishing returns (log) so one viral tweet doesn't dominate,
// and replies/RTs count more than likes (they signal real discussion).
export function engagementScore(t) {
  const m = t.metrics;
  const raw = m.likes + m.retweets * 3 + m.replies * 2 + m.quotes * 2 + m.impressions * 0.001;
  return Math.log10(raw + 1);
}

export function accountWeight(handle, weights) {
  const key = String(handle || '').toLowerCase();
  return weights[key] ?? weights.default ?? 1.0;
}

export function scoreTweet(t, weights) {
  return engagementScore(t) * accountWeight(t.handle, weights);
}

// --- noise detection -------------------------------------------------------

export function isNoise(t, filters) {
  if (filters.dropRetweets && t.isRetweet) return true;

  const lowEngagement =
    t.metrics.likes < (filters.minLikes ?? 0) &&
    t.metrics.impressions < (filters.minImpressions ?? 0);
  if (lowEngagement) return true;

  const text = t.text.toLowerCase();
  for (const kw of filters.noiseKeywords || []) {
    if (text.includes(kw.toLowerCase())) return true;
  }
  return false;
}

// --- deduplication ---------------------------------------------------------

function normalizeText(s) {
  return s
    .toLowerCase()
    .replace(/https?:\/\/\S+/g, ' ')
    .replace(/[^a-z0-9\s]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
}

function shingles(s) {
  return new Set(normalizeText(s).split(' ').filter(Boolean));
}

function jaccard(a, b) {
  if (a.size === 0 || b.size === 0) return 0;
  let inter = 0;
  for (const w of a) if (b.has(w)) inter++;
  return inter / (a.size + b.size - inter);
}

// Removes near-duplicates, keeping the higher-scored tweet of each pair.
// Assumes input is already sorted by score descending.
export function dedupe(sortedTweets, threshold) {
  const kept = [];
  const keptShingles = [];
  for (const t of sortedTweets) {
    const sh = shingles(t.text);
    const dup = keptShingles.some((ks) => jaccard(sh, ks) >= threshold);
    if (!dup) {
      kept.push(t);
      keptShingles.push(sh);
    }
  }
  return kept;
}

// --- orchestration ---------------------------------------------------------

// Returns { kept, newCursor, stats }. `sinceId` drops already-processed tweets (incremental).
export function applyFilters(tweets, config, sinceId = null) {
  const { filters, accountWeights, maxItems } = config;
  const total = tweets.length;

  // 1. incremental cursor: skip anything we've already seen
  let rows = sinceId ? tweets.filter((t) => safeGt(t.id, sinceId)) : tweets.slice();
  const afterCursor = rows.length;

  // 2. drop noise
  rows = rows.filter((t) => !isNoise(t, filters));
  const afterNoise = rows.length;

  // 3. score + sort
  rows.forEach((t) => { t.score = scoreTweet(t, accountWeights); });
  rows.sort((a, b) => b.score - a.score);

  // 4. near-duplicate merge
  rows = dedupe(rows, filters.nearDuplicateThreshold ?? 0.8);
  const afterDedupe = rows.length;

  // 5. trim to budget
  const kept = rows.slice(0, maxItems);

  // new cursor = highest id we saw this run (so next run starts after it)
  const newCursor = tweets.reduce((max, t) => (safeGt(t.id, max) ? t.id : max), sinceId || '0');

  return {
    kept,
    newCursor,
    stats: { total, afterCursor, afterNoise, afterDedupe, kept: kept.length },
  };
}

function safeGt(a, b) {
  try { return BigInt(a) > BigInt(b); } catch { return String(a) > String(b); }
}
