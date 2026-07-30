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

// A quote-tweet is not a retweet. xapi appends the quoted post as a marker block instead of
// replacing the text, so the "RT @" prefix check missed quote-tweets entirely and a bare-emoji
// quote-tweet read as an original post competing on its author's full engagement.
func TestObserveDetectsQuoteTweets(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		isRetweet bool
		isQuote   bool
		isReply   bool
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
			name:    "quote tweet with a comment",
			text:    "this is the part everyone is missing\n[quoting @orig: the story being quoted]",
			isQuote: true,
		},
		{
			name:    "bare emoji quote tweet",
			text:    "😂\n[quoting @orig: the story being quoted]",
			isQuote: true,
		},
		{
			name:    "reply",
			text:    "strong disagree with this take\n[replying to @orig: the claim]",
			isReply: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := observe(sources.Tweet{ID: "1", Handle: "@a", Text: c.text})

			if got.IsRetweet != c.isRetweet {
				t.Errorf("isRetweet = %v, want %v", got.IsRetweet, c.isRetweet)
			}
			if got.IsQuote != c.isQuote {
				t.Errorf("isQuote = %v, want %v", got.IsQuote, c.isQuote)
			}
			if got.IsReply != c.isReply {
				t.Errorf("isReply = %v, want %v", got.IsReply, c.isReply)
			}
		})
	}
}

// ownText isolates the words the author actually wrote. Engagement measures audience size,
// never substance; a post whose own text is empty after removing borrowed blocks, URLs and
// bare mentions has nothing for a text digest to summarize, however popular it is.
func TestOwnTextKeepsOnlyTheAuthorsWords(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{
			name: "plain post is untouched",
			text: "Anthropic shipped a new model today",
			want: "Anthropic shipped a new model today",
		},
		{
			name: "quoted block is not the author's words",
			text: "this is the part everyone is missing\n[quoting @orig: the story being quoted]",
			want: "this is the part everyone is missing",
		},
		{
			name: "bare emoji quote tweet contributes one character",
			text: "😂\n[quoting @orig: a long story about something consequential]",
			want: "😂",
		},
		{
			name: "replied-to block is not the author's words",
			text: "strong disagree with this take\n[replying to @orig: the claim being answered]",
			want: "strong disagree with this take",
		},
		{
			name: "a retweet contributes nothing",
			text: "RT @orig: something worth repeating",
			want: "",
		},
		{
			name: "a retweet of a quote tweet still contributes nothing",
			text: "RT @orig: their words\n[quoting @third: the underlying source]",
			want: "",
		},
		{
			name: "urls are not words",
			text: "the paper is here https://t.co/abc123",
			want: "the paper is here",
		},
		{
			name: "a bare link is empty",
			text: "https://t.co/abc123",
			want: "",
		},
		{
			name: "plain http links are stripped too",
			text: "old school http://example.com/story",
			want: "old school",
		},
		{
			name: "mentions are addressing, not substance",
			text: "@FirstSquawk this is already priced in",
			want: "this is already priced in",
		},
		{
			name: "whitespace collapses",
			text: "  spaced   out\n words ",
			want: "spaced out words",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ownText(c.text); got != c.want {
				t.Errorf("ownText = %q, want %q", got, c.want)
			}
		})
	}
}

// Own-text length is counted in characters for the same reason TextLength is: the digest
// runs in Korean, where byte length triples.
func TestObserveCountsOwnTextInCharacters(t *testing.T) {
	korean := "인공지능 모델이 오늘 공개되었다"
	text := korean + "\n[quoting @orig: the story being quoted]"

	got := observe(sources.Tweet{ID: "1", Text: text})

	want := len([]rune(korean))
	if got.OwnTextLength != want {
		t.Errorf("ownTextLength = %d, want %d runes (bytes would be %d)", got.OwnTextLength, want, len(korean))
	}
}

// OwnTextLength is a new signal, not a replacement: TextLength still measures everything the
// model is shown, including the quoted post, which is what the prompt actually costs.
func TestObserveKeepsFullTextLengthAlongsideOwnText(t *testing.T) {
	text := "😂\n[quoting @orig: a long story about something consequential]"

	got := observe(sources.Tweet{ID: "1", Text: text})

	if got.TextLength != len([]rune(text)) {
		t.Errorf("textLength = %d, want %d (the whole post, quoted block included)", got.TextLength, len([]rune(text)))
	}
	if got.OwnTextLength != 1 {
		t.Errorf("ownTextLength = %d, want 1", got.OwnTextLength)
	}
	if got.OwnTextLength >= got.TextLength {
		t.Errorf("ownTextLength %d should be shorter than textLength %d", got.OwnTextLength, got.TextLength)
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
