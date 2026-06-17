// sources package defines the input interace for automations and the slim data
// type they consume. Concrete sources (mock now; bird, xquik later) implement Source.
package sources

import "context"

// Twwet is one post from a source. Deliberately minimal: only the fields the digest needs (filtering/ summarizing/ verifiability).
// not raw API payloads.
// Keeping it slim is the "filter/trim before the model" habit applied to data
type Tweet struct {
	ID      string
	Author  string
	Handle  string
	Text    string
	URL     string
	Likes   int
	Reposts int
}

// Source is a swappable provider of tweets. Everything downstream depends on this interface,
// never on a concrete source, so mock/bird/xquik are drop-in
type Source interface {
	Name() string
	Fetch(ctx context.Context) ([]Tweet, error)
}
