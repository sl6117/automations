// Package corpus keeps every fetched post, kept or dropped, so later passes (mentions,
// attention, backfills of new extractors) can run over data already paid for instead of
// re-buying reads. Stored text is private: nothing in this package exports it.
package corpus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/sl6117/automations/internal/storage"
	"github.com/sl6117/automations/pkg/sources"
)

const (
	PostSchemaVersion = "corpus.post.v1"
	RunSchemaVersion  = "corpus.run.v1"

	// DefaultTable is the corpus table when CORPUS_TABLE is unset. It is a separate
	// table so the main table's full-table List scans never read corpus items.
	DefaultTable = "automations-corpus"

	postsPrefix = "corpus/posts/"
	runsPrefix  = "corpus/runs/"
	dayLayout   = "2006-01-02"

	// maxChunkBytes caps one Append. A Dynamo item is limited to 400 KB including its
	// keys and attribute names; half that leaves room for a post that overshoots.
	maxChunkBytes = 200 * 1024
)

// Ref mirrors sources.Ref with stable lower-camel keys.
type Ref struct {
	Type         string `json:"type"`
	ID           string `json:"id"`
	AuthorID     string `json:"authorId,omitempty"`
	AuthorHandle string `json:"authorHandle,omitempty"`
	Text         string `json:"text,omitempty"`
}

// StoredPost is one post as fetched. The metrics are a snapshot at FetchedAt; the same
// post fetched again later is a new line, and Load keeps the latest.
type StoredPost struct {
	ID              string    `json:"id"`
	Author          string    `json:"author"`
	Handle          string    `json:"handle"`
	Text            string    `json:"text"`
	URL             string    `json:"url"`
	Likes           int       `json:"likes"`
	Reposts         int       `json:"reposts"`
	Replies         int       `json:"replies"`
	Quotes          int       `json:"quotes"`
	Bookmarks       int       `json:"bookmarks"`
	Impressions     int       `json:"impressions"`
	CreatedAt       time.Time `json:"createdAt"`
	AuthorFollowers int       `json:"authorFollowers"`
	AuthorID        string    `json:"authorId,omitempty"`
	OwnText         string    `json:"ownText,omitempty"`
	Refs            []Ref     `json:"refs,omitempty"`

	SourceKeys    []string  `json:"sourceKeys"`
	FetchedAt     time.Time `json:"fetchedAt"`
	Kept          bool      `json:"kept"`
	DropReason    string    `json:"dropReason,omitempty"`
	Backfilled    bool      `json:"backfilled,omitempty"`
	SchemaVersion string    `json:"schemaVersion"`
}

// RunRecord is one fetch. Written below Fetched means the day is partially stored,
// which an export must be able to see rather than mistake for a quiet day.
type RunRecord struct {
	FetchedAt     time.Time `json:"fetchedAt"`
	Source        string    `json:"source"`
	Reads         int       `json:"reads"`
	Truncated     bool      `json:"truncated"`
	Fetched       int       `json:"fetched"`
	Written       int       `json:"written"`
	SchemaVersion string    `json:"schemaVersion"`
}

// FromTweet copies every Tweet field. Copied rather than embedded because Tweet has no
// JSON tags, and this stored format should not inherit Go-name keys.
func FromTweet(t sources.Tweet) StoredPost {
	p := StoredPost{
		ID: t.ID, Author: t.Author, Handle: t.Handle, Text: t.Text, URL: t.URL,
		Likes: t.Likes, Reposts: t.Reposts, Replies: t.Replies, Quotes: t.Quotes,
		Bookmarks: t.Bookmarks, Impressions: t.Impressions, CreatedAt: t.CreatedAt,
		AuthorFollowers: t.AuthorFollowers, AuthorID: t.AuthorID, OwnText: t.OwnText,
	}
	for _, r := range t.Refs {
		p.Refs = append(p.Refs, Ref(r))
	}
	return p
}

// Tweet converts back, for passes that already work on sources.Tweet.
func (p StoredPost) Tweet() sources.Tweet {
	t := sources.Tweet{
		ID: p.ID, Author: p.Author, Handle: p.Handle, Text: p.Text, URL: p.URL,
		Likes: p.Likes, Reposts: p.Reposts, Replies: p.Replies, Quotes: p.Quotes,
		Bookmarks: p.Bookmarks, Impressions: p.Impressions, CreatedAt: p.CreatedAt,
		AuthorFollowers: p.AuthorFollowers, AuthorID: p.AuthorID, OwnText: p.OwnText,
	}
	for _, r := range p.Refs {
		t.Refs = append(t.Refs, sources.Ref(r))
	}
	return t
}

// PostsKey and RunsKey partition by the UTC date of the fetch, not of the post: a day's
// file is what we knew by then, which is what point-in-time work needs.
func PostsKey(day time.Time) string { return postsPrefix + day.UTC().Format(dayLayout) + ".jsonl" }
func RunsKey(day time.Time) string  { return runsPrefix + day.UTC().Format(dayLayout) + ".jsonl" }

// StoreFromEnv selects the corpus store: the Dynamo table named by CORPUS_TABLE when
// STORAGE_BACKEND=dynamo, otherwise the filesystem under AUTOMATION_ROOT.
func StoreFromEnv(ctx context.Context) (storage.Store, error) {
	if os.Getenv("STORAGE_BACKEND") == "dynamo" {
		table := os.Getenv("CORPUS_TABLE")
		if table == "" {
			table = DefaultTable
		}
		return storage.NewDynamoTable(ctx, table)
	}
	return storage.NewFS(), nil
}

// Save writes the posts, then the run record with how many posts actually landed. Every
// post is stamped with the run's FetchedAt so one fetch never straddles two partitions.
// It keeps going after a failed chunk and returns the joined errors once done.
func Save(ctx context.Context, store storage.Store, run RunRecord, posts []StoredPost) error {
	if run.FetchedAt.IsZero() {
		return errors.New("corpus save: run has no FetchedAt")
	}
	run.FetchedAt = run.FetchedAt.UTC()
	run.SchemaVersion = RunSchemaVersion
	key := PostsKey(run.FetchedAt)

	var errs []error
	var chunk [][]byte
	chunkBytes := 0
	written := 0
	flush := func() {
		if len(chunk) == 0 {
			return
		}
		if err := store.Append(ctx, key, bytes.Join(chunk, []byte("\n"))); err != nil {
			errs = append(errs, fmt.Errorf("corpus append %d posts: %w", len(chunk), err))
		} else {
			written += len(chunk)
		}
		chunk, chunkBytes = nil, 0
	}
	for _, p := range posts {
		p.FetchedAt = run.FetchedAt
		p.SchemaVersion = PostSchemaVersion
		line, err := json.Marshal(p)
		if err != nil {
			errs = append(errs, fmt.Errorf("corpus marshal post %s: %w", p.ID, err))
			continue
		}
		if len(chunk) > 0 && chunkBytes+1+len(line) > maxChunkBytes {
			flush()
		}
		chunk = append(chunk, line)
		chunkBytes += len(line) + 1
	}
	flush()

	run.Written = written
	line, err := json.Marshal(run)
	if err == nil {
		err = store.Append(ctx, RunsKey(run.FetchedAt), line)
	}
	if err != nil {
		errs = append(errs, fmt.Errorf("corpus run record: %w", err))
	}
	return errors.Join(errs...)
}

// Load returns the posts fetched on each UTC day in [fromDay, toDay], once per post id
// (the latest FetchedAt wins), sorted by numeric id. A day with no file is empty.
func Load(ctx context.Context, store storage.Store, fromDay, toDay time.Time) ([]StoredPost, error) {
	latest := map[string]StoredPost{}
	err := eachLine(ctx, store, fromDay, toDay, PostsKey, func(key string, n int, line []byte) error {
		var p StoredPost
		if err := json.Unmarshal(line, &p); err != nil {
			return fmt.Errorf("corpus %s line %d: %w", key, n, err)
		}
		if prev, ok := latest[p.ID]; !ok || p.FetchedAt.After(prev.FetchedAt) {
			latest[p.ID] = p
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	posts := make([]StoredPost, 0, len(latest))
	for _, p := range latest {
		posts = append(posts, p)
	}
	sort.Slice(posts, func(i, j int) bool { return idLess(posts[i].ID, posts[j].ID) })
	return posts, nil
}

// LoadRuns returns every run record in the range, oldest fetch first.
func LoadRuns(ctx context.Context, store storage.Store, fromDay, toDay time.Time) ([]RunRecord, error) {
	var runs []RunRecord
	err := eachLine(ctx, store, fromDay, toDay, RunsKey, func(key string, n int, line []byte) error {
		var r RunRecord
		if err := json.Unmarshal(line, &r); err != nil {
			return fmt.Errorf("corpus %s line %d: %w", key, n, err)
		}
		runs = append(runs, r)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].FetchedAt.Before(runs[j].FetchedAt) })
	return runs, nil
}

// eachLine reads each day's key directly. Never List: on Dynamo that is a full-table scan.
func eachLine(ctx context.Context, store storage.Store, fromDay, toDay time.Time, keyFor func(time.Time) string, fn func(key string, n int, line []byte) error) error {
	from := utcDate(fromDay)
	to := utcDate(toDay)
	if from.After(to) {
		return fmt.Errorf("corpus range: %s is after %s", from.Format(dayLayout), to.Format(dayLayout))
	}
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		key := keyFor(day)
		data, err := store.Get(ctx, key)
		if errors.Is(err, storage.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("corpus read %s: %w", key, err)
		}
		for i, line := range bytes.Split(data, []byte("\n")) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			if err := fn(key, i+1, line); err != nil {
				return err
			}
		}
	}
	return nil
}

func utcDate(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// idLess orders X ids numerically (they are snowflakes, so numeric order is time order).
// A non-numeric id sorts after every numeric one, then lexically, so order stays total.
func idLess(a, b string) bool {
	na, errA := strconv.ParseUint(a, 10, 64)
	nb, errB := strconv.ParseUint(b, 10, 64)
	switch {
	case errA == nil && errB == nil:
		return na < nb
	case errA == nil:
		return true
	case errB == nil:
		return false
	default:
		return a < b
	}
}
