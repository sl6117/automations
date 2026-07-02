package sources

import (
	"context"
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
	if gotExpansions != "author_id" {
		t.Errorf("expansions = %q, want author_id", gotExpansions)
	}
	if sawSinceID {
		t.Errorf("since_id is sent even though SinceID is empty")
	}

}

func TestXAPIFetchSinceID(t *testing.T) {
	var gotSinceID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSinceID = r.URL.Query().Get("since_id")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data": [], "includes": {"users": []}}`)
	}))
	defer server.Close()

	x := XAPI{BearerToken: "test-token", ListID: "12345", BaseURL: server.URL, SinceID: "2072532278476148881"}

	if _, err := x.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if gotSinceID != "2072532278476148881" {
		t.Errorf("since_id = %q, want 2072532278476148881", gotSinceID)
	}
}
