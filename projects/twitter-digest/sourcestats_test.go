package twitterdigest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sl6117/automations/internal/storage"
	"github.com/sl6117/automations/pkg/sources"
)

func TestObserveDetectsPostShape(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		isRetweet bool
		isReply   bool
		hasLink   bool
	}{
		{
			name: "plain post",
			text: "Anthropic shipped a new model today",
		},
		{
			name:      "retweet",
			text:      "RT @orig: something worth repeating",
			isRetweet: true,
		},
		{
			name:    "reply",
			text:    "Strong disagree with this take\n[replying to @orig: the claim being replied to]",
			isReply: true,
		},
		{
			name:    "carries a link",
			text:    "the paper is here https://t.co/abc123",
			hasLink: true,
		},
		{
			name:    "plain http link",
			text:    "old school http://example.com/story",
			hasLink: true,
		},
		{
			name:      "retweet carrying a link",
			text:      "RT @orig: read this https://t.co/xyz",
			isRetweet: true,
			hasLink:   true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := observe(sources.Tweet{ID: "1", Handle: "@a", Text: c.text})

			if got.IsRetweet != c.isRetweet {
				t.Errorf("isRetweet = %v, want %v", got.IsRetweet, c.isRetweet)
			}
			if got.IsReply != c.isReply {
				t.Errorf("isReply = %v, want %v", got.IsReply, c.isReply)
			}
			if got.HasLink != c.hasLink {
				t.Errorf("hasLink = %v, want %v", got.HasLink, c.hasLink)
			}
		})
	}
}

// Length is in characters, not bytes: the digest runs in Korean as well as English, and
// byte length would make every CJK post look three times longer than it is.
func TestObserveCountsCharactersNotBytes(t *testing.T) {
	korean := "인공지능 모델이 오늘 공개되었다"

	got := observe(sources.Tweet{ID: "1", Text: korean})

	want := len([]rune(korean))
	if got.TextLength != want {
		t.Errorf("textLength = %d, want %d runes (bytes would be %d)", got.TextLength, want, len(korean))
	}
	if got.IsLongForm {
		t.Error("a short Korean post must not read as long-form")
	}
}

func TestObserveFlagsLongForm(t *testing.T) {
	short := observe(sources.Tweet{ID: "1", Text: strings.Repeat("a", 280)})
	if short.IsLongForm {
		t.Error("280 characters is not yet long-form")
	}

	long := observe(sources.Tweet{ID: "2", Text: strings.Repeat("a", 281)})
	if !long.IsLongForm {
		t.Error("281 characters should read as long-form (note_tweet expansion)")
	}
}

func TestObserveCopiesMetricsAndTime(t *testing.T) {
	created := time.Date(2026, 7, 27, 12, 30, 45, 0, time.UTC)

	got := observe(sources.Tweet{
		ID: "100", Handle: "@darioa", Text: "hello", CreatedAt: created,
		Likes: 500, Reposts: 30, Replies: 12, Quotes: 7,
		Bookmarks: 63, Impressions: 91000, AuthorFollowers: 250000,
	})

	if got.ID != "100" || got.Handle != "@darioa" {
		t.Errorf("id/handle = %q/%q, want 100/@darioa", got.ID, got.Handle)
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("createdAt = %v, want %v", got.CreatedAt, created)
	}
	if got.Likes != 500 || got.Reposts != 30 || got.Replies != 12 || got.Quotes != 7 {
		t.Errorf("metrics not copied: %+v", got)
	}
	if got.Bookmarks != 63 || got.Impressions != 91000 || got.AuthorFollowers != 250000 {
		t.Errorf("wider metrics not copied: %+v", got)
	}
	// observe records shape only; the verdict is filter's to set
	if got.Kept || got.DropReason != "" {
		t.Errorf("observe must not decide a verdict: kept=%v reason=%q", got.Kept, got.DropReason)
	}
}

func TestSaveSourceStatsWritesOneBlobPerRun(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}
	ctx := context.Background()

	obs := []PostObservation{
		{ID: "1", Handle: "@a", Likes: 500, Kept: true},
		{ID: "2", Handle: "@a", Likes: 1, DropReason: DropLowEngagement},
		{ID: "3", Handle: "@b", Likes: 500, Kept: true},
	}

	meta := FetchMeta{Source: "xapi", Reads: 150, Truncated: true}
	if err := saveSourceStats(ctx, store, meta, obs); err != nil {
		t.Fatalf("saveSourceStats: %v", err)
	}

	keys, err := store.List(ctx, "logs/sourcestats/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("got %d keys, want 1: %v", len(keys), keys)
	}
	if !strings.HasPrefix(keys[0], "logs/sourcestats/") || !strings.HasSuffix(keys[0], "-twitter-digest.json") {
		t.Errorf("key = %q, want logs/sourcestats/<timestamp>-twitter-digest.json", keys[0])
	}

	data, err := store.Get(ctx, keys[0])
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var got SourceStats
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Fetched != 3 {
		t.Errorf("fetched = %d, want 3", got.Fetched)
	}
	if got.Kept != 2 {
		t.Errorf("kept = %d, want 2", got.Kept)
	}
	// the mock source must be identifiable so dev runs can be excluded from the analysis
	if got.Source != "xapi" {
		t.Errorf("source = %q, want xapi", got.Source)
	}
	if got.Timestamp == "" {
		t.Error("timestamp missing")
	}
	// reads and truncation live with the rows so cost per kept post, and how much of the
	// day was silently dropped, are answerable from a single blob
	if got.Reads != 150 {
		t.Errorf("reads = %d, want 150", got.Reads)
	}
	if !got.Truncated {
		t.Error("truncated = false, want true")
	}
	if len(got.Posts) != 3 {
		t.Fatalf("got %d posts, want 3", len(got.Posts))
	}
	if got.Posts[1].DropReason != DropLowEngagement {
		t.Errorf("drop reason did not survive the round trip: %+v", got.Posts[1])
	}
}

func TestSaveSourceStatsRecordsEmptyFetches(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}
	ctx := context.Background()

	// a fetch that returned nothing is still a fact worth having: it distinguishes a
	// quiet day from a broken cursor
	if err := saveSourceStats(ctx, store, FetchMeta{Source: "xapi", Reads: 50}, nil); err != nil {
		t.Fatalf("saveSourceStats: %v", err)
	}

	keys, err := store.List(ctx, "logs/sourcestats/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("got %d keys, want 1: %v", len(keys), keys)
	}

	data, err := store.Get(ctx, keys[0])
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var got SourceStats
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Fetched != 0 || got.Kept != 0 {
		t.Errorf("fetched/kept = %d/%d, want 0/0", got.Fetched, got.Kept)
	}
}
