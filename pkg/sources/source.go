// sources package defines the input interace for automations and the slim data
// type they consume. Concrete sources (mock now; bird, xquik later) implement Source.
package sources

import (
	"context"
	"time"
)

// Twwet is one post from a source. Deliberately minimal: only the fields the digest needs (filtering/ summarizing/ verifiability).
// not raw API payloads.
// Keeping it slim is the "filter/trim before the model" habit applied to data
type Tweet struct {
	ID              string
	Author          string
	Handle          string
	Text            string
	URL             string
	Likes           int
	Reposts         int
	Replies         int
	Quotes          int
	Bookmarks       int
	Impressions     int
	CreatedAt       time.Time // zero when the API omits or malforms created_at: age unknown
	AuthorFollowers int

	// The fields below keep the structure Text flattens away, so later passes can tell
	// the author's words from a referenced post's. They are tagged omitempty (unlike the
	// fields above, which are stored under their Go names) so artifacts that embed
	// Tweet do not grow for sources that leave them empty.
	AuthorID string `json:",omitempty"`
	// OwnText is what the author wrote, as the API sent it: URLs and mentions intact,
	// no RT/quote/reply markers. Empty for a retweet, where the author wrote nothing.
	OwnText string `json:",omitempty"`
	Refs    []Ref  `json:",omitempty"`
}

// Ref is a post this one retweets, quotes or replies to. When the referenced post was
// not in the API's includes (deleted or protected), only Type and ID are set.
type Ref struct {
	Type         string `json:",omitempty"` // the API's value: retweeted, quoted or replied_to
	ID           string `json:",omitempty"`
	AuthorID     string `json:",omitempty"`
	AuthorHandle string `json:",omitempty"` // "@name", like Tweet.Handle; empty when the author is unknown
	Text         string `json:",omitempty"` // full text, preferring note_tweet like Tweet.Text
}

// Source is a swappable provider of tweets. Everything downstream depends on this interface,
// never on a concrete source, so mock/bird/xquik are drop-in
type Source interface {
	Name() string
	Fetch(ctx context.Context) ([]Tweet, error)
}
