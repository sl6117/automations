package sources

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
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
	// author - comes back empty
	if gotUserFields != "name,username" {
		t.Errorf("user.fields = %q, want name,username", gotUserFields)
	}
	if gotExpansions != "author_id,referenced_tweets.id,referenced_tweets.id.author_id" {
		t.Errorf("expansions = %q, want referenced tweet expansions", gotExpansions)
	}
	if sawSinceID {
		t.Errorf("since_id is sent even though SinceID is empty")
	}

}

// func TestXAPIFetchSinceID(t *testing.T) {
// 	var gotSinceID string

// 	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		gotSinceID = r.URL.Query().Get("since_id")
// 		w.Header().Set("Content-Type", "application/json")
// 		io.WriteString(w, `{"data": [], "includes": {"users": []}}`)
// 	}))
// 	defer server.Close()

// 	x := XAPI{BearerToken: "test-token", ListID: "12345", BaseURL: server.URL, SinceID: "2072532278476148881"}

//		if _, err := x.Fetch(context.Background()); err != nil {
//			t.Fatalf("Fetch returned error: %v", err)
//		}
//		if gotSinceID != "2072532278476148881" {
//			t.Errorf("since_id = %q, want 2072532278476148881", gotSinceID)
//		}
//	}
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
	if gotTweetFields != "public_metrics,referenced_tweets,note_tweet" {
		t.Errorf("tweet.fields = %q, want note_tweet requested", gotTweetFields)
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
