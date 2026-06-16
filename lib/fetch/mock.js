// Offline sample data so the whole pipeline runs with no credentials and no network.
// Deliberately includes: multiple topics, a near-duplicate pair, a low-engagement post,
// and a retweet — to exercise the filter and the digest.

import { normalizeTweet } from './index.js';

const NOW = Date.now();
const hoursAgo = (h) => new Date(NOW - h * 3600_000).toISOString();

const SAMPLE = [
  {
    id: '1001', author: 'Andrej Karpathy', handle: 'karpathy',
    text: 'New open-weights model drops today: matches GPT-4 class on reasoning benchmarks while running on a single consumer GPU. Weights + paper linked.',
    createdAt: hoursAgo(2), likes: 9800, retweets: 1500, replies: 320, impressions: 1200000, lang: 'en',
  },
  {
    id: '1002', author: 'AI News Bot', handle: 'ainewsbot',
    text: 'BREAKING: new open model out today, runs on one GPU, GPT-4 level. (thread)',
    createdAt: hoursAgo(2), likes: 40, retweets: 8, replies: 2, impressions: 5000, lang: 'en',
  },
  {
    id: '1003', author: 'Lex Fridman', handle: 'lexfridman',
    text: 'Long conversation with a leading economist on why inflation expectations are decoupling from headline CPI, and what it means for rates in 2026.',
    createdAt: hoursAgo(5), likes: 5400, retweets: 600, replies: 410, impressions: 800000, lang: 'en',
  },
  {
    id: '1004', author: 'Federal Reserve', handle: 'federalreserve',
    text: 'The FOMC released the minutes of its latest meeting. Members noted resilient labor markets and signaled patience on rate cuts.',
    createdAt: hoursAgo(7), likes: 2100, retweets: 900, replies: 800, impressions: 1500000, lang: 'en',
  },
  {
    id: '1005', author: 'Vitalik Buterin', handle: 'VitalikButerin',
    text: 'Thoughts on the latest L2 fee-market changes: blob pricing is finally behaving as designed, and rollup costs dropped ~40% week over week.',
    createdAt: hoursAgo(3), likes: 7200, retweets: 1100, replies: 540, impressions: 900000, lang: 'en',
  },
  {
    id: '1006', author: 'CoinDesk', handle: 'CoinDesk',
    text: 'Spot ETF inflows hit a 3-month high as institutional desks rotate back in. Full data and charts in the report.',
    createdAt: hoursAgo(6), likes: 1800, retweets: 420, replies: 130, impressions: 600000, lang: 'en',
  },
  {
    id: '1007', author: 'Crypto Moonboy', handle: 'moonboy100x',
    text: '🚀🚀 THIS COIN WILL 100X TRUST ME 🚀🚀 dont miss out ser, link in bio, financial freedom incoming',
    createdAt: hoursAgo(1), likes: 12, retweets: 3, replies: 1, impressions: 800, lang: 'en',
  },
  {
    id: '1008', author: 'Random User', handle: 'randomguy',
    text: 'RT @someone: gm everyone hope you have a great day',
    createdAt: hoursAgo(4), likes: 5, retweets: 0, replies: 0, impressions: 200, lang: 'en',
  },
  {
    id: '1009', author: 'Stratechery', handle: 'stratechery',
    text: 'The real story in big tech earnings is capex on AI data centers outpacing revenue growth. The bet is enormous and the timeline is long.',
    createdAt: hoursAgo(8), likes: 3300, retweets: 510, replies: 220, impressions: 700000, lang: 'en',
  },
  {
    id: '1010', author: 'The Economist', handle: 'TheEconomist',
    text: 'Global trade is quietly rerouting around new tariffs. Our analysis of shipping data shows the shifts are larger than headline numbers suggest.',
    createdAt: hoursAgo(9), likes: 2600, retweets: 700, replies: 190, impressions: 1100000, lang: 'en',
  },
];

export function createMockFetcher() {
  return {
    name: 'mock',
    async fetchTweets({ sinceId = null, limit = 100 } = {}) {
      let rows = SAMPLE.map(normalizeTweet);
      if (sinceId) rows = rows.filter((t) => BigInt(t.id) > BigInt(sinceId));
      return rows.slice(0, limit);
    },
  };
}
