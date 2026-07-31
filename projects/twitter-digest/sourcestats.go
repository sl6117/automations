package twitterdigest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sl6117/automations/internal/storage"
	"github.com/sl6117/automations/pkg/sources"
)

// DropReason says why filter rejected a post. Empty means the post was kept.
type DropReason string

const (
	sourceStatsPrefix            = "logs/sourcestats/"
	DropLowEngagement DropReason = "low_engagement"
	DropDuplicate     DropReason = "duplicate"
	DropPerAuthorCap  DropReason = "per_author_cap"
	DropEmptyOwnText  DropReason = "empty_own_text"
	DropDigestBudget  DropReason = "digest_budget"
)

// PostObservation is the feature row for one fetched post: every metric the fetch paid
// for, cheap structural facts, and the filter's verdict. Dropped posts are recorded too,
// so a scoring rubric can be fitted and replayed offline instead of re-buying reads.
type PostObservation struct {
	ID              string     `json:"id"`
	Handle          string     `json:"handle"`
	CreatedAt       time.Time  `json:"createdAt"`
	Likes           int        `json:"likes"`
	Reposts         int        `json:"reposts"`
	Replies         int        `json:"replies"`
	Quotes          int        `json:"quotes"`
	Bookmarks       int        `json:"bookmarks"`
	Impressions     int        `json:"impressions"`
	AuthorFollowers int        `json:"authorFollowers"`
	TextLength      int        `json:"textLength"`
	OwnTextLength   int        `json:"ownTextLength"`
	HasLink         bool       `json:"hasLink"`
	IsRetweet       bool       `json:"isRetweet"`
	IsQuote         bool       `json:"isQuote"`
	IsReply         bool       `json:"isReply"`
	IsLongForm      bool       `json:"isLongForm"`
	Kept            bool       `json:"kept"`
	DropReason      DropReason `json:"dropReason,omitempty"`
}

// engagement is the signal both the low-engagement floor and the per-author cap rank on.
func (p PostObservation) engagement() int { return p.Likes + p.Reposts }

// score is engagement per follower: cold-start stand-in for a per-handle rolling median.
// Within one author the divisor is constant, so score order equals engagement order and
// the per-author cap keeps its current behaviour.
// Across authors it stops a mega-account's causal posts from crowding out a mid-size account's real hit.
// Zero/missing followers degrade to divisor 1 rather than rejecting the post.
func (p PostObservation) score() float64 {
	followers := p.AuthorFollowers
	if followers < 1 {
		followers = 1
	}
	return float64(p.engagement()) / float64(followers)
}

// FetchMeta is the run-level context for a set of observations: which source produced them,
// what they cost, and whehter the page cap cut the window short.
type FetchMeta struct {
	Source    string
	Reads     int
	Truncated bool
}

// SourceStats is one run's fetch record.
type SourceStats struct {
	Timestamp string            `json:"timestamp"`
	Source    string            `json:"source"`
	Reads     int               `json:"reads"`
	Truncated bool              `json:"truncated"`
	Fetched   int               `json:"fetched"`
	Kept      int               `json:"kept"`
	Posts     []PostObservation `json:"posts"`
}

// Markers that xapi.tweetsFromPage writes into a post's text when it carries someone
// else's: everything from a marker onward belongs to the quoted or replied-to post.
const (
	retweetPrefix = "RT @"
	quoteMarker   = "\n[quoting @"
	replyMarker   = "\n[replying to @"
)

// ownText returns the words the author actually wrote: the post with borrowed blocks, URLs
// and bare mentions removed. A retweet returns empty because the author wrote none of it.
// Engagement measures audience size and popularity, never substance, so a post that reduces
// to nothing here has no reporting in it however well it performed.
func ownText(text string) string {
	body := text
	for _, marker := range []string{quoteMarker, replyMarker} {
		if i := strings.Index(body, marker); i >= 0 {
			body = body[:i]
		}
	}
	if strings.HasPrefix(body, retweetPrefix) {
		return ""
	}
	fields := strings.Fields(body)
	words := make([]string, 0, len(fields))
	for _, w := range fields {
		if strings.HasPrefix(w, "http://") || strings.HasPrefix(w, "https://") || strings.HasPrefix(w, "@") {
			continue
		}
		words = append(words, w)
	}
	return strings.Join(words, " ")
}

// observe records a post's shape, leaving the verdict fields zero for filter to set.
// Retweets, quote-tweets and replies are detected from the markers xapi.tweetsFromPage writes into Text.
func observe(t sources.Tweet) PostObservation {
	length := utf8.RuneCountInString(t.Text)

	return PostObservation{
		ID:              t.ID,
		Handle:          t.Handle,
		CreatedAt:       t.CreatedAt,
		Likes:           t.Likes,
		Reposts:         t.Reposts,
		Replies:         t.Replies,
		Quotes:          t.Quotes,
		Bookmarks:       t.Bookmarks,
		Impressions:     t.Impressions,
		AuthorFollowers: t.AuthorFollowers,
		TextLength:      length,
		OwnTextLength:   utf8.RuneCountInString(ownText(t.Text)),
		HasLink:         strings.Contains(t.Text, "http://") || strings.Contains(t.Text, "https://"),
		IsRetweet:       strings.HasPrefix(t.Text, retweetPrefix),
		IsQuote:         strings.Contains(t.Text, quoteMarker),
		IsReply:         strings.Contains(t.Text, replyMarker),
		IsLongForm:      length > 280,
	}
}

// saveSourceStats writes one blob per run under logs/sourcestats/. Keyed by timestamp
// rather than date so a same-day retry cannot overwrite the earlier record.
func saveSourceStats(ctx context.Context, store storage.Store, meta FetchMeta, obs []PostObservation) error {
	now := time.Now().UTC()

	stats := SourceStats{
		Timestamp: now.Format(time.RFC3339),
		Source:    meta.Source,
		Reads:     meta.Reads,
		Truncated: meta.Truncated,
		Fetched:   len(obs),
		Posts:     obs,
	}
	for _, o := range obs {
		if o.Kept {
			stats.Kept++
		}
	}

	data, err := json.MarshalIndent(stats, "", " ")
	if err != nil {
		return fmt.Errorf("marshal source stats: %w", err)
	}
	key := sourceStatsPrefix + now.Format("2006-01-02T15-04-05Z") + "-twitter-digest.json"
	if err := store.Put(ctx, key, data); err != nil {
		return fmt.Errorf("write source stats: %w", err)
	}
	return nil
}
