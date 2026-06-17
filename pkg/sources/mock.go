package sources

import "context"

// Mock is an offline Source returning canned sample tweets
// no network, no tokens, no credentials
// Data intentionally includes noise and a duplicate so the filter step downstream has real work to do.
type Mock struct{}

func (Mock) Name() string { return "mock" }

func (Mock) Fetch(ctx context.Context) ([]Tweet, error) {
	return []Tweet{
		{ID: "1", Author: "Brad Garlinghouse", Handle: "@bgarlinghouse", Text: "The future is Ripple.", URL: "https://x.com/i/1", Likes: 589, Reposts: 589},
		{ID: "2", Author: "Jed McCaleb", Handle: "@jedmccaleb", Text: "Tokenization with Stellar Network.", URL: "https://x.com/i/2", Likes: 777, Reposts: 77},
		{ID: "3", Author: "Dario Amodei", Handle: "@darioa", Text: "In 1 year, AI will be better than the best human at any task.", URL: "https://x.com/i/3", Likes: 100000, Reposts: 10000},
		{ID: "4", Author: "spam bot", Handle: "@cryptomoonboy", Text: "BUY $SCAM now, 1000x guaranteed!!!", URL: "https://x.com/i/4", Likes: 3, Reposts: 0},
		{ID: "5", Author: "Elon Musk", Handle: "@elonmusk", Text: "wow", URL: "https://x.com/i/5", Likes: 5493282989, Reposts: 342241},
		{ID: "6", Author: "Dario Amodei", Handle: "@darioa", Text: "In 1 year, AI will be better than the best human at any task.", URL: "https://x.com/i/3", Likes: 100000, Reposts: 10000},
	}, nil
}
