package sources

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestXAPIFetch(t *testing.T) {
	var gotAuth, gotPath, gotUserFields, gotExpansions string
	sawSinceID := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		sawSinceID = r.URL.Query().Has("since_id")

		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotUserFields = r.URL.Query().Get("user.fields")
		gotExpansions = r.URL.Query().Get("expansions")

		w.Header().Set("Content-Type", "application/json")

		io.WriteString(w, `{
			"data": [
				{"id": "100", "text": "AI is moving fast", "author_id": "42",
				 "public_metrics": {"like_count": 500, "retweet_count": 30}}
			],
			"includes": {
			"users": [
			{"id": "42", "name": "Dario Amodei", "username": "darioa"}
			]
			}
		}`)
	}))

	defer server.Close()

	x := XAPI{BearerToken: "test-token", ListID: "12345", BaseURL: server.URL}
	tweets, err := x.Fetch(context.Background())

	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	if len(tweets) != 1 {
		t.Fatalf("got %d tweets, want 1", len(tweets))
	}

	want := Tweet{
		ID:      "100",
		Author:  "Dario Amodei",
		Handle:  "@darioa",
		Text:    "AI is moving fast",
		URL:     "https://x.com/darioa/status/100",
		Likes:   500,
		Reposts: 30,
	}
	if tweets[0] != want {
		t.Errorf("tweet =\n %+v\nwant\n %+v", tweets[0], want)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-token")
	}
	if gotPath != "/lists/12345/tweets" {
		t.Errorf("path = %q, want /lists/12345/tweets", gotPath)
	}

	// regression guard for user.fields typo -> without this param the API returns no name/username
	// author - comes back empty. public_metrics carries followers_count, which costs nothing
	// extra: the user objects are already expanded
	if gotUserFields != "name,username,public_metrics" {
		t.Errorf("user.fields = %q, want name,username,public_metrics", gotUserFields)
	}
	if gotExpansions != "author_id,referenced_tweets.id,referenced_tweets.id.author_id" {
		t.Errorf("expansions = %q, want referenced tweet expansions", gotExpansions)
	}
	if sawSinceID {
		t.Errorf("since_id is sent even though SinceID is empty")
	}

}

func TestXAPIFetchSinceID(t *testing.T) {
	sawSinceID := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawSinceID = r.URL.Query().Has("since_id")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"data": [
				{"id": "300", "text": "newer than cursor", "author_id": "42",
				 "public_metrics": {"like_count": 1, "retweet_count": 0}},
				{"id": "200", "text": "exactly the cursor", "author_id": "42",
				 "public_metrics": {"like_count": 1, "retweet_count": 0}},
				{"id": "100", "text": "older than cursor", "author_id": "42",
				 "public_metrics": {"like_count": 1, "retweet_count": 0}}
			],
			"includes": {"users": [{"id": "42", "name": "Author", "username": "author"}]}
		}`)
	}))
	defer server.Close()
	x := XAPI{BearerToken: "test-token", ListID: "12345", BaseURL: server.URL, SinceID: "200"}
	tweets, err := x.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	// the real endpoint 400s on since_id — it must never be sent
	if sawSinceID {
		t.Error("since_id param sent; list-tweets endpoint rejects it, filter client-side")
	}
	if len(tweets) != 1 || tweets[0].ID != "300" {
		t.Errorf("got %d tweets (first id %q), want only id 300", len(tweets), tweets[0].ID)
	}
}

func TestXAPIFetchExpandsRetweets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"data": [
				{"id": "200", "text": "RT @orig: this gets cut mid-sen",
				 "author_id": "42",
				 "public_metrics": {"like_count": 500, "retweet_count": 30},
				 "referenced_tweets": [{"type": "retweeted", "id": "199"}]},
				{"id": "201", "text": "RT @ghost: original was dele",
				 "author_id": "42",
				 "public_metrics": {"like_count": 300, "retweet_count": 10},
				 "referenced_tweets": [{"type": "retweeted", "id": "198"}]}
			],
			"includes": {
				"users": [
					{"id": "42", "name": "Retweeter", "username": "retweeter"},
					{"id": "77", "name": "Original Author", "username": "orig"}
				],
				"tweets": [
					{"id": "199", "text": "this gets cut mid-sentence in the wrapper but the expansion has every word", "author_id": "77"}
				]
			}
		}`)
	}))
	defer server.Close()

	x := XAPI{BearerToken: "test-token", ListID: "12345", BaseURL: server.URL}
	tweets, err := x.Fetch(context.Background())

	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if len(tweets) != 2 {
		t.Fatalf("got %d tweets, want 2", len(tweets))
	}

	want := "RT @orig: this gets cut mid-sentence in the wrapper but the expansion has every word"

	if tweets[0].Text != want {
		t.Errorf("expanded text = %q, want %q", tweets[0].Text, want)
	}
	// original missing from includes -> keep truncated wrapper text
	if tweets[1].Text != "RT @ghost: original was dele" {
		t.Errorf("fallback text = %q, want truncated wrapper text", tweets[1].Text)
	}
}

func TestXAPIFetchExpandsQuotes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"data": [
				{"id": "300", "text": "This changes everything for agents",
				 "author_id": "42",
				 "public_metrics": {"like_count": 500, "retweet_count": 30},
				 "referenced_tweets": [{"type": "quoted", "id": "299"}]}
			],
			"includes": {
				"users": [
					{"id": "42", "name": "Commenter", "username": "commenter"},
					{"id": "77", "name": "Original Author", "username": "orig"}
				],
				"tweets": [
					{"id": "299", "text": "We are releasing a new model today", "author_id": "77"}
				]
			}
		}`)
	}))
	defer server.Close()
	x := XAPI{BearerToken: "test-token", ListID: "12345", BaseURL: server.URL}
	tweets, err := x.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if len(tweets) != 1 {
		t.Fatalf("got %d tweets, want 1", len(tweets))
	}
	want := "This changes everything for agents\n[quoting @orig: We are releasing a new model today]"
	if tweets[0].Text != want {
		t.Errorf("quoted text = %q, want %q", tweets[0].Text, want)
	}
}

func TestXAPIFetchPaginatesUntilCursor(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("pagination_token") == "" {
			io.WriteString(w, `{
				"data": [
					{"id": "500", "text": "newest", "author_id": "42", "public_metrics": {"like_count": 1, "retweet_count": 0}},
					{"id": "400", "text": "newer", "author_id": "42", "public_metrics": {"like_count": 1, "retweet_count": 0}}
				],
				"includes": {"users": [{"id": "42", "name": "Author", "username": "author"}]},
				"meta": {"next_token": "page2"}
			}`)
			return
		}
		io.WriteString(w, `{
			"data": [
				{"id": "300", "text": "older but unseen", "author_id": "42", "public_metrics": {"like_count": 1, "retweet_count": 0}},
				{"id": "200", "text": "the cursor itself", "author_id": "42", "public_metrics": {"like_count": 1, "retweet_count": 0}}
			],
			"includes": {"users": [{"id": "42", "name": "Author", "username": "author"}]},
			"meta": {"next_token": "page3-must-never-be-fetched"}
		}`)
	}))

	defer server.Close()

	x := XAPI{BearerToken: "test-token", ListID: "12345", BaseURL: server.URL, SinceID: "200"}
	tweets, err := x.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if requests != 2 {
		t.Errorf("made %d requests, want 2 (cursor reached on page 2)", requests)
	}
	if len(tweets) != 3 {
		t.Errorf("got %d tweets, want 3", len(tweets))
	}
}

func TestXAPIFetchCountsReads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("pagination_token") == "" {
			io.WriteString(w, `{
				"data": [{"id": "500", "text": "newest", "author_id": "42", "public_metrics": {"like_count": 1, "retweet_count": 0}}],
				"includes": {"users": [{"id": "42", "name": "Author", "username": "author"}]},
				"meta": {"next_token": "page2"}
			}`)
			return
		}
		io.WriteString(w, `{
			"data": [{"id": "200", "text": "the cursor itself", "author_id": "42", "public_metrics": {"like_count": 1, "retweet_count": 0}}],
			"includes": {"users": [{"id": "42", "name": "Author", "username": "author"}]}
		}`)
	}))
	defer server.Close()
	var reads int
	x := XAPI{BearerToken: "test-token", ListID: "12345", BaseURL: server.URL, SinceID: "200", Reads: &reads}
	if _, err := x.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	// 2 pages fetched, 50 billed reads each
	if reads != 100 {
		t.Errorf("reads = %d, want 100", reads)
	}
}

func TestXAPIFetchFirstRunFetchesOnePage(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"data": [{"id": "100", "text": "hello", "author_id": "42", "public_metrics": {"like_count": 1, "retweet_count": 0}}],
			"includes": {"users": [{"id": "42", "name": "Author", "username": "author"}]},
			"meta": {"next_token": "tempting-second-page"}
		}`)
	}))
	defer server.Close()
	x := XAPI{BearerToken: "test-token", ListID: "12345", BaseURL: server.URL}
	if _, err := x.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if requests != 1 {
		t.Errorf("made %d requests, want 1 (no cursor = single page)", requests)
	}
}

func TestFetchSpendCapIsTypeQuotaError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"detail":"Your monthly spend cap has been reached."}`))
	}))
	defer server.Close()

	x := XAPI{BearerToken: "t", ListID: "1", BaseURL: server.URL}

	if _, err := x.Fetch(context.Background()); !errors.Is(err, ErrQuota) {
		t.Fatalf("want ErrQuota, got %v", err)
	}
}

func TestXAPIFetchUsesNoteTweetFullText(t *testing.T) {
	var gotTweetFields string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTweetFields = r.URL.Query().Get("tweet.fields")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"data": [
				{"id": "400", "text": "long analysis cut mid-sen",
				 "note_tweet": {"text": "long analysis cut mid-sentence? not anymore: the note has the whole argument"},
				 "author_id": "42",
				 "public_metrics": {"like_count": 9, "retweet_count": 2}},
				{"id": "401", "text": "RT @orig: the original long post gets c",
				 "author_id": "42",
				 "public_metrics": {"like_count": 5, "retweet_count": 1},
				 "referenced_tweets": [{"type": "retweeted", "id": "399"}]}
			],
			"includes": {
				"users": [
					{"id": "42", "name": "Analyst", "username": "analyst"},
					{"id": "77", "name": "Original", "username": "orig"}
				],
				"tweets": [
					{"id": "399", "text": "the original long post gets capped too",
					 "note_tweet": {"text": "the original long post gets capped too, unless we read its note_tweet as well"},
					 "author_id": "77"}
				]
			}
		}`)
	}))
	defer server.Close()

	x := XAPI{BearerToken: "test-token", ListID: "12345", BaseURL: server.URL}
	tweets, err := x.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	// regression guard: without note_tweet in tweet.fields the API omits the full text entirely
	if gotTweetFields != "public_metrics,referenced_tweets,note_tweet,created_at" {
		t.Errorf("tweet.fields = %q, want note_tweet and created_at requested", gotTweetFields)
	}
	if len(tweets) != 2 {
		t.Fatalf("got %d tweets, want 2", len(tweets))
	}
	if want := "long analysis cut mid-sentence? not anymore: the note has the whole argument"; tweets[0].Text != want {
		t.Errorf("data note_tweet: text = %q, want full note", tweets[0].Text)
	}
	if want := "RT @orig: the original long post gets capped too, unless we read its note_tweet as well"; tweets[1].Text != want {
		t.Errorf("include note_tweet: text = %q, want RT wrapper with full note", tweets[1].Text)
	}
}

func TestXAPIFetchExpandsReplies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"data": [
				{"id": "300", "text": "Strong disagree with this take",
				 "author_id": "42",
				 "public_metrics": {"like_count": 500, "retweet_count": 30},
				 "referenced_tweets": [{"type": "replied_to", "id": "299"}]}
			],
			"includes": {
				"users": [
					{"id": "42", "name": "Commenter", "username": "commenter"},
					{"id": "77", "name": "Original Author", "username": "orig"}
				],
				"tweets": [
					{"id": "299", "text": "the claim being replied to", "author_id": "77"}
				]
			}
		}`)
	}))
	defer server.Close()
	x := XAPI{BearerToken: "test-token", ListID: "12345", BaseURL: server.URL}
	tweets, err := x.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if len(tweets) != 1 {
		t.Fatalf("got %d tweets, want 1", len(tweets))
	}
	want := "Strong disagree with this take\n[replying to @orig: the claim being replied to]"
	if tweets[0].Text != want {
		t.Errorf("reply text = %q, want %q", tweets[0].Text, want)
	}
}

// The wider metric set and created_at ride along on posts we already pay for, so every
// field the API hands back should reach Tweet rather than being dropped on the floor.
func TestXAPIFetchParsesWiderMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"data": [
				{"id": "100", "text": "AI is moving fast", "author_id": "42",
				 "created_at": "2026-07-27T12:30:45.000Z",
				 "public_metrics": {"like_count": 500, "retweet_count": 30,
				  "reply_count": 12, "quote_count": 7, "bookmark_count": 63,
				  "impression_count": 91000}}
			],
			"includes": {
				"users": [
					{"id": "42", "name": "Dario Amodei", "username": "darioa",
					 "public_metrics": {"followers_count": 250000}}
				]
			}
		}`)
	}))
	defer server.Close()

	x := XAPI{BearerToken: "test-token", ListID: "12345", BaseURL: server.URL}
	tweets, err := x.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if len(tweets) != 1 {
		t.Fatalf("got %d tweets, want 1", len(tweets))
	}

	got := tweets[0]
	if got.Replies != 12 {
		t.Errorf("Replies = %d, want 12", got.Replies)
	}
	if got.Quotes != 7 {
		t.Errorf("Quotes = %d, want 7", got.Quotes)
	}
	if got.Bookmarks != 63 {
		t.Errorf("Bookmarks = %d, want 63", got.Bookmarks)
	}
	if got.Impressions != 91000 {
		t.Errorf("Impressions = %d, want 91000", got.Impressions)
	}
	if got.AuthorFollowers != 250000 {
		t.Errorf("AuthorFollowers = %d, want 250000", got.AuthorFollowers)
	}
	wantTime := time.Date(2026, 7, 27, 12, 30, 45, 0, time.UTC)
	if !got.CreatedAt.Equal(wantTime) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, wantTime)
	}
	// the original two metrics must keep working
	if got.Likes != 500 || got.Reposts != 30 {
		t.Errorf("Likes/Reposts = %d/%d, want 500/30", got.Likes, got.Reposts)
	}
}

// bookmark_count and impression_count are not guaranteed on every post or every access
// tier. A response without them must parse to zeroes, never an error: a fetch that fails
// is a day of digests lost and 150 billed reads wasted.
func TestXAPIFetchToleratesMissingWiderFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"data": [
				{"id": "100", "text": "no wider metrics here", "author_id": "42",
				 "public_metrics": {"like_count": 5, "retweet_count": 1}}
			],
			"includes": {"users": [{"id": "42", "name": "Author", "username": "author"}]}
		}`)
	}))
	defer server.Close()

	x := XAPI{BearerToken: "test-token", ListID: "12345", BaseURL: server.URL}
	tweets, err := x.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if len(tweets) != 1 {
		t.Fatalf("got %d tweets, want 1", len(tweets))
	}

	got := tweets[0]
	if got.Bookmarks != 0 || got.Impressions != 0 || got.Replies != 0 || got.Quotes != 0 {
		t.Errorf("absent metrics should be zero, got %+v", got)
	}
	if got.AuthorFollowers != 0 {
		t.Errorf("AuthorFollowers = %d, want 0 when the API omits user public_metrics", got.AuthorFollowers)
	}
	if !got.CreatedAt.IsZero() {
		t.Errorf("CreatedAt = %v, want the zero time when created_at is absent", got.CreatedAt)
	}
	if got.Likes != 5 || got.Reposts != 1 {
		t.Errorf("Likes/Reposts = %d/%d, want 5/1", got.Likes, got.Reposts)
	}
}

// Same reasoning as the missing-field case: a date we cannot parse degrades to "age
// unknown" rather than killing the run.
func TestXAPIFetchToleratesUnparseableCreatedAt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"data": [
				{"id": "100", "text": "bad date", "author_id": "42",
				 "created_at": "yesterday afternoon",
				 "public_metrics": {"like_count": 5, "retweet_count": 1}}
			],
			"includes": {"users": [{"id": "42", "name": "Author", "username": "author"}]}
		}`)
	}))
	defer server.Close()

	x := XAPI{BearerToken: "test-token", ListID: "12345", BaseURL: server.URL}
	tweets, err := x.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if len(tweets) != 1 {
		t.Fatalf("got %d tweets, want 1", len(tweets))
	}
	if !tweets[0].CreatedAt.IsZero() {
		t.Errorf("CreatedAt = %v, want the zero time for an unparseable date", tweets[0].CreatedAt)
	}
}

// The page cap binding is silent data loss: pagination is newest-first and the cursor
// advances to the newest id regardless, so the posts past the last fetched page are never
// retrieved and never will be. The flag is how we find out it happened.
func TestXAPIFetchFlagsTruncationAtThePageCap(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		// every page offers another: the list always has more than the cap allows
		io.WriteString(w, `{
			"data": [{"id": "900", "text": "more where this came from", "author_id": "42",
			 "public_metrics": {"like_count": 1, "retweet_count": 0}}],
			"includes": {"users": [{"id": "42", "name": "Author", "username": "author"}]},
			"meta": {"next_token": "always-another-page"}
		}`)
	}))
	defer server.Close()

	var truncated bool
	x := XAPI{
		BearerToken: "test-token", ListID: "12345", BaseURL: server.URL,
		SinceID: "1", MaxPages: 2, Truncated: &truncated,
	}
	if _, err := x.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	if requests != 2 {
		t.Errorf("made %d requests, want 2 (the cap)", requests)
	}
	if !truncated {
		t.Error("truncated = false, want true: the cap bound with more pages available")
	}
}

func TestXAPIFetchDoesNotFlagTruncationWhenCursorReached(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("pagination_token") == "" {
			io.WriteString(w, `{
				"data": [{"id": "500", "text": "newest", "author_id": "42", "public_metrics": {"like_count": 1, "retweet_count": 0}}],
				"includes": {"users": [{"id": "42", "name": "Author", "username": "author"}]},
				"meta": {"next_token": "page2"}
			}`)
			return
		}
		io.WriteString(w, `{
			"data": [{"id": "200", "text": "the cursor itself", "author_id": "42", "public_metrics": {"like_count": 1, "retweet_count": 0}}],
			"includes": {"users": [{"id": "42", "name": "Author", "username": "author"}]},
			"meta": {"next_token": "there-is-more-but-we-are-caught-up"}
		}`)
	}))
	defer server.Close()

	var truncated bool
	x := XAPI{
		BearerToken: "test-token", ListID: "12345", BaseURL: server.URL,
		SinceID: "200", MaxPages: 5, Truncated: &truncated,
	}
	if _, err := x.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	// caught up with the cursor: everything new was retrieved, nothing was lost
	if truncated {
		t.Error("truncated = true, want false: the fetch reached the cursor")
	}
}

func TestXAPIFetchDoesNotFlagTruncationWhenTheListRunsOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"data": [{"id": "500", "text": "the only page", "author_id": "42", "public_metrics": {"like_count": 1, "retweet_count": 0}}],
			"includes": {"users": [{"id": "42", "name": "Author", "username": "author"}]}
		}`)
	}))
	defer server.Close()

	var truncated bool
	x := XAPI{
		BearerToken: "test-token", ListID: "12345", BaseURL: server.URL,
		SinceID: "1", MaxPages: 5, Truncated: &truncated,
	}
	if _, err := x.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if truncated {
		t.Error("truncated = true, want false: the list had no further pages")
	}
}

// Truncated is optional, exactly like Reads: a caller that does not care must not crash.
func TestXAPIFetchTruncationFlagIsOptional(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"data": [{"id": "900", "text": "more", "author_id": "42", "public_metrics": {"like_count": 1, "retweet_count": 0}}],
			"includes": {"users": [{"id": "42", "name": "Author", "username": "author"}]},
			"meta": {"next_token": "always-another-page"}
		}`)
	}))
	defer server.Close()

	x := XAPI{BearerToken: "test-token", ListID: "12345", BaseURL: server.URL, SinceID: "1", MaxPages: 2}
	if _, err := x.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
}

// A retweet's follower count and timestamp must describe the retweeting account, not the
// original author: recency is when it crossed our list, and the audience is the one that
// actually engaged with this post.
func TestXAPIFetchAttributesMetadataToTheRetweeter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"data": [
				{"id": "200", "text": "RT @orig: something worth repeating",
				 "author_id": "42",
				 "created_at": "2026-07-27T09:00:00.000Z",
				 "public_metrics": {"like_count": 2, "retweet_count": 0},
				 "referenced_tweets": [{"type": "retweeted", "id": "199"}]}
			],
			"includes": {
				"users": [
					{"id": "42", "name": "Retweeter", "username": "retweeter",
					 "public_metrics": {"followers_count": 1000}},
					{"id": "77", "name": "Original Author", "username": "orig",
					 "public_metrics": {"followers_count": 900000}}
				],
				"tweets": [
					{"id": "199", "text": "something worth repeating", "author_id": "77"}
				]
			}
		}`)
	}))
	defer server.Close()

	x := XAPI{BearerToken: "test-token", ListID: "12345", BaseURL: server.URL}
	tweets, err := x.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if len(tweets) != 1 {
		t.Fatalf("got %d tweets, want 1", len(tweets))
	}
	if tweets[0].AuthorFollowers != 1000 {
		t.Errorf("AuthorFollowers = %d, want the retweeter's 1000", tweets[0].AuthorFollowers)
	}
	wantTime := time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC)
	if !tweets[0].CreatedAt.Equal(wantTime) {
		t.Errorf("CreatedAt = %v, want the retweet time %v", tweets[0].CreatedAt, wantTime)
	}
}
