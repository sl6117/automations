package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	defaultXAPIBaseURL = "https://api.x.com/2"
	defaultMaxPages    = 3 // cap on pages per fetch: each page is 50 billed reads
)

// ErrQuota marks non-transient quota/billing refusals (the x monthly spend cap)
// Callers detect it with errors.Is: retrying a spend cap just delays
// the same failure, so it should alert and fail fast instead
var ErrQuota = errors.New("quota exhausted")

// XAPI is a Source backed by the X (Twitter) API v2 list-timeline endpoint
// Read-only, app-only Bearer auth. BaseURL is overridable so tests point at httptest
type XAPI struct {
	BearerToken string
	ListID      string
	SinceID     string // client-side cursor: drop tweets at/below this id (endpoint has no since_id param)
	MaxPages    int
	Reads       *int         // optional billed-read counter: +50 per fetched page (nil = don't count)
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
		NoteTweet struct {
			Text string `json:"text"`
		} `json:"note_tweet"`
	} `json:"data"`

	Includes struct {
		Users []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Username string `json:"username"`
		} `json:"users"`
		Tweets []struct {
			ID        string `json:"id"`
			Text      string `json:"text"`
			AuthorID  string `json:"author_id"`
			NoteTweet struct {
				Text string `json:"text"`
			} `json:"note_tweet"`
		} `json:"tweets"`
	} `json:"includes"`
	Meta struct {
		NextToken string `json:"next_token"`
	} `json:"meta"`
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

	maxPages := x.MaxPages
	if maxPages <= 0 {
		maxPages = defaultMaxPages
	}
	if x.SinceID == "" {
		maxPages = 1
	}

	var tweets []Tweet
	nextToken := ""

	for page := 0; page < maxPages; page++ {
		parsed, err := x.fetchPage(ctx, httpClient, base, nextToken)
		if err != nil {
			return nil, err
		}
		if x.Reads != nil {
			*x.Reads += 50
		}

		pageTweets, reachedCursor := x.tweetsFromPage(parsed)
		tweets = append(tweets, pageTweets...)

		if reachedCursor || parsed.Meta.NextToken == "" {
			break
		}
		nextToken = parsed.Meta.NextToken
	}

	return tweets, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// fullText prefers the note_tweet text the API sends for long (>280 char) posts;
// the plain text field arrives capped mid-sentence for those
func fullText(text, note string) string {
	if note != "" {
		return note
	}
	return text
}

func idNewer(a, b string) bool {
	na, errA := strconv.ParseUint(a, 10, 64)
	nb, errB := strconv.ParseUint(b, 10, 64)

	if errA != nil || errB != nil {
		return true
	}
	return na > nb
}

func (x XAPI) fetchPage(ctx context.Context, httpClient *http.Client, base, paginationToken string) (xListTweetsResponse, error) {
	var parsed xListTweetsResponse

	q := url.Values{}
	q.Set("max_results", "50")
	q.Set("tweet.fields", "public_metrics,referenced_tweets,note_tweet")
	q.Set("expansions", "author_id,referenced_tweets.id,referenced_tweets.id.author_id")
	q.Set("user.fields", "name,username")
	if paginationToken != "" {
		q.Set("pagination_token", paginationToken)
	}

	endpoint := fmt.Sprintf("%s/lists/%s/tweets?%s", base, x.ListID, q.Encode())

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)

	if err != nil {
		return parsed, fmt.Errorf("build x request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+x.BearerToken)

	resp, err := httpClient.Do(httpReq)

	if err != nil {
		return parsed, fmt.Errorf("call x api: %w", err)
	}

	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)

	if err != nil {
		return parsed, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusForbidden {
		return parsed, fmt.Errorf("x api %d: %s: %w", resp.StatusCode, truncate(string(data), 300), ErrQuota)
	}

	if resp.StatusCode != http.StatusOK {
		return parsed, fmt.Errorf("x api %d: %s", resp.StatusCode, truncate(string(data), 300))
	}

	if err := json.Unmarshal(data, &parsed); err != nil {
		return parsed, fmt.Errorf("unmarshal x response: %w", err)
	}

	return parsed, nil
}

// tweetsFromPage converts one API page to Tweets. reachedCursor reports whether the page contained any tweet at/below the sinceID
// pages are newest-first, so once true, every following page is already seen
func (x XAPI) tweetsFromPage(parsed xListTweetsResponse) (tweets []Tweet, reachedCursor bool) {

	type user struct{ name, username string }

	users := make(map[string]user, len(parsed.Includes.Users))

	for _, u := range parsed.Includes.Users {
		users[u.ID] = user{name: u.Name, username: u.Username}
	}

	type refTweet struct{ text, authorID string }

	refs := make(map[string]refTweet, len(parsed.Includes.Tweets))

	for _, rt := range parsed.Includes.Tweets {
		refs[rt.ID] = refTweet{text: fullText(rt.Text, rt.NoteTweet.Text), authorID: rt.AuthorID}
	}

	for _, d := range parsed.Data {
		if x.SinceID != "" && !idNewer(d.ID, x.SinceID) {
			reachedCursor = true
			continue
		}
		author := users[d.AuthorID]

		text := fullText(d.Text, d.NoteTweet.Text)
		for _, ref := range d.ReferencedTweets {

			original, ok := refs[ref.ID]
			if !ok {
				continue // referenced tweet missing from includes (Deleted/protected): keep wrapper text
			}
			originalUser := users[original.authorID]
			switch ref.Type {
			case "retweeted":
				text = "RT @" + originalUser.username + ": " + original.text
			case "quoted":
				text += "\n[quoting @" + originalUser.username + ": " + original.text + "]"
			case "replied_to":
				text += "\n[replying to @" + originalUser.username + ": " + original.text + "]"
			}
		}
		tweets = append(tweets, Tweet{
			ID:      d.ID,
			Author:  author.name,
			Handle:  "@" + author.username,
			Text:    text,
			URL:     fmt.Sprintf("https://x.com/%s/status/%s", author.username, d.ID),
			Likes:   d.PublicMetrics.LikeCount,
			Reposts: d.PublicMetrics.RetweetCount,
		})
	}
	return tweets, reachedCursor
}
