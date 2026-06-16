// Data-source interface. Every adapter returns tweets in ONE normalized shape, so the
// rest of the pipeline never cares whether the data came from bird, Xquik, or a mock.
//
// Normalized tweet:
// {
//   id: string,            // unique tweet id
//   author: string,        // display name
//   handle: string,        // without leading @
//   text: string,
//   url: string,           // canonical https://x.com/<handle>/status/<id>
//   createdAt: string,     // ISO 8601
//   metrics: { likes, retweets, replies, quotes, impressions }, // numbers (0 if unknown)
//   isRetweet: boolean,
//   lang: string           // best-effort language code, may be ''
// }
//
// Adapter contract:
//   createFetcher(name) -> { name, async fetchTweets({ listId, sinceId, limit }) -> Tweet[] }

import { createMockFetcher } from './mock.js';
import { createBirdFetcher } from './bird.js';

export function createFetcher(name) {
  switch ((name || 'mock').toLowerCase()) {
    case 'mock':
      return createMockFetcher();
    case 'bird':
      return createBirdFetcher();
    // case 'xquik':  // future: drop in without touching the pipeline
    //   return createXquikFetcher();
    default:
      throw new Error(`Unknown FETCH_SOURCE "${name}". Use "mock" or "bird".`);
  }
}

// Helper adapters can use to guarantee the normalized shape.
export function normalizeTweet(raw) {
  const handle = String(raw.handle || raw.username || '').replace(/^@/, '');
  const id = String(raw.id || raw.id_str || raw.tweetId || '');
  const metrics = raw.metrics || {};
  return {
    id,
    author: String(raw.author || raw.name || raw.displayName || handle || 'unknown'),
    handle,
    text: String(raw.text || raw.full_text || raw.content || '').trim(),
    url: raw.url || (handle && id ? `https://x.com/${handle}/status/${id}` : ''),
    createdAt: raw.createdAt || raw.created_at || raw.date || '',
    metrics: {
      likes: num(metrics.likes ?? raw.likes ?? raw.favorite_count),
      retweets: num(metrics.retweets ?? raw.retweets ?? raw.retweet_count),
      replies: num(metrics.replies ?? raw.replies ?? raw.reply_count),
      quotes: num(metrics.quotes ?? raw.quotes ?? raw.quote_count),
      impressions: num(metrics.impressions ?? raw.impressions ?? raw.views ?? raw.view_count),
    },
    isRetweet: Boolean(raw.isRetweet ?? raw.is_retweet ?? /^RT @/.test(raw.text || '')),
    lang: String(raw.lang || raw.language || ''),
  };
}

function num(v) {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}
