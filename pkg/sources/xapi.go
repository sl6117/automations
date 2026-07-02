package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const defaultXAPIBaseURL = "https://api.x.com/2"

// XAPI is a Source backed by the X (Twitter) API v2 list-timeline endpoint
// Read-only, app-only Bearer auth. BaseURL is overridable so tests point at httptest
type XAPI struct {
	BearerToken string
	ListID      string
	SinceID     string       // fetch tweets newer than this ID
	BaseURL     string       // defaults to defaultXAPIBaseURL
	HTTPClient  *http.Client // defaults to a client with sane timeout
}

func (x XAPI) Name() string { return "xapi" }

// xListTweetsResponse is the slice of the X v2 list-tweets payload we use.
// Private to this file: the project never sees raw API shapes, only []Tweet
type xListTweetsResponse struct {
	Data []struct {
		ID            string `json:"id"`
		Text          string `json:"text"`
		AuthorID      string `json:"author_id"`
		PublicMetrics struct {
			LikeCount    int `json:"like_count"`
			RetweetCount int `json:"retweet_count"`
		} `json:"public_metrics"`
		ReferencedTweets []struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"referenced_tweets"`
	} `json:"data"`

	Includes struct {
		Users []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Username string `json:"username"`
		} `json:"users"`
		Tweets []struct {
			ID       string `json:"id"`
			Text     string `json:"text"`
			AuthorID string `json:"author_id"`
		} `json:"tweets"`
	} `json:"includes"`
}

func (x XAPI) Fetch(ctx context.Context) ([]Tweet, error) {
	base := x.BaseURL
	if base == "" {
		base = defaultXAPIBaseURL
	}
	httpClient := x.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	q := url.Values{}
	q.Set("max_results", "50")
	q.Set("tweet.fields", "public_metrics,referenced_tweets")
	q.Set("expansions", "author_id,referenced_tweets.id,referenced_tweets.id.author_id")
	q.Set("user.fields", "name,username")

	if x.SinceID != "" {
		q.Set("since_id", x.SinceID)
	}

	endpoint := fmt.Sprintf("%s/lists/%s/tweets?%s", base, x.ListID, q.Encode())

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)

	if err != nil {
		return nil, fmt.Errorf("build x request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+x.BearerToken)

	resp, err := httpClient.Do(httpReq)

	if err != nil {
		return nil, fmt.Errorf("call x api: %w", err)
	}

	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("x api %d: %s", resp.StatusCode, truncate(string(data), 300))
	}

	var parsed xListTweetsResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal x response: %w", err)
	}

	type user struct{ name, username string }

	users := make(map[string]user, len(parsed.Includes.Users))

	for _, u := range parsed.Includes.Users {
		users[u.ID] = user{name: u.Name, username: u.Username}
	}

	type refTweet struct{ text, authorID string }

	refs := make(map[string]refTweet, len(parsed.Includes.Tweets))

	for _, rt := range parsed.Includes.Tweets {
		refs[rt.ID] = refTweet{text: rt.Text, authorID: rt.AuthorID}
	}

	tweets := make([]Tweet, 0, len(parsed.Data))

	for _, d := range parsed.Data {
		user := users[d.AuthorID]

		text := d.Text
		for _, ref := range d.ReferencedTweets {
			if ref.Type != "retweeted" {
				continue
			}
			orig, ok := refs[ref.ID]
			if !ok {
				continue
			}
			origUser := users[orig.authorID]
			text = "RT @" + origUser.username + ": " + orig.text
		}
		tweets = append(tweets, Tweet{
			ID:      d.ID,
			Author:  user.name,
			Handle:  "@" + user.username,
			Text:    text,
			URL:     fmt.Sprintf("https://x.com/%s/status/%s", user.username, d.ID),
			Likes:   d.PublicMetrics.LikeCount,
			Reposts: d.PublicMetrics.RetweetCount,
		})
	}
	return tweets, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
