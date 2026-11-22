package corpus

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sl6117/automations/internal/storage"
	"github.com/sl6117/automations/pkg/sources"
)

var (
	day1 = time.Date(2026, 11, 20, 16, 0, 0, 0, time.UTC)
	day2 = time.Date(2026, 11, 21, 16, 0, 0, 0, time.UTC)
	day3 = time.Date(2026, 11, 22, 16, 0, 0, 0, time.UTC)
)

// countingStore records the size of every Append and fails the calls numbered in failOn.
type countingStore struct {
	storage.Store
	appends map[string][]int // key -> byte size of each successful append
	failOn  map[int]bool     // 1-based Append call numbers that fail
	calls   int
}

func newCountingStore(t *testing.T, failOn ...int) *countingStore {
	c := &countingStore{Store: &storage.FS{Root: t.TempDir()}, appends: map[string][]int{}, failOn: map[int]bool{}}
	for _, n := range failOn {
		c.failOn[n] = true
	}
	return c
}

func (c *countingStore) Append(ctx context.Context, key string, line []byte) error {
	c.calls++
	if c.failOn[c.calls] {
		return errors.New("synthetic write failure")
	}
	c.appends[key] = append(c.appends[key], len(line))
	return c.Store.Append(ctx, key, line)
}

func post(id, text string) StoredPost {
	return StoredPost{ID: id, Handle: "@example_wire", Text: text, URL: "https://x.com/example_wire/status/" + id, SourceKeys: []string{"core"}}
}

func fullTweet() sources.Tweet {
	return sources.Tweet{
		ID: "1900000000000000003", Author: "Example Wire", Handle: "@example_wire",
		Text:  "Not convinced\n[quoting @example_desk: $EXMPL guidance raised]",
		URL:   "https://x.com/example_wire/status/1900000000000000003",
		Likes: 10, Reposts: 2, Replies: 3, Quotes: 4, Bookmarks: 5, Impressions: 600,
		CreatedAt: time.Date(2026, 11, 21, 15, 0, 0, 0, time.UTC), AuthorFollowers: 700,
		AuthorID: "1900000000000000901",
		OwnText:  "Not convinced",
		Refs: []sources.Ref{{Type: "quoted", ID: "1900000000000000012", AuthorID: "1900000000000000902",
			AuthorHandle: "@example_desk", Text: "$EXMPL guidance raised"}},
	}
}

// A field added to sources.Tweet later must not be silently dropped from the corpus.
func TestStoredPostCarriesEveryTweetField(t *testing.T) {
	stored := reflect.TypeOf(StoredPost{})
	tweet := reflect.TypeOf(sources.Tweet{})
	for i := 0; i < tweet.NumField(); i++ {
		name := tweet.Field(i).Name
		if _, ok := stored.FieldByName(name); !ok {
			t.Errorf("StoredPost has no %s: new Tweet fields must be stored", name)
		}
	}
	tw := fullTweet()
	for i := 0; i < tweet.NumField(); i++ {
		if reflect.ValueOf(tw).Field(i).IsZero() {
			t.Fatalf("fixture leaves %s zero, so the round trip below cannot prove it is copied", tweet.Field(i).Name)
		}
	}
	if got := FromTweet(tw).Tweet(); !reflect.DeepEqual(got, tw) {
		t.Errorf("round trip =\n %+v\nwant\n %+v", got, tw)
	}
}

func TestSaveThenLoadKeepsEveryField(t *testing.T) {
	ctx := context.Background()
	store := &storage.FS{Root: t.TempDir()}
	p := FromTweet(fullTweet())
	p.SourceKeys = []string{"core"}
	p.DropReason = "low_engagement"
	if err := Save(ctx, store, RunRecord{FetchedAt: day2, Source: "xapi", Reads: 50, Fetched: 1}, []StoredPost{p}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(ctx, store, day2, day2)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := p
	want.FetchedAt = day2
	want.SchemaVersion = PostSchemaVersion
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Errorf("Load =\n %+v\nwant\n %+v", got, want)
	}
	raw, err := store.Get(ctx, PostsKey(day2))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("typical StoredPost line (quote with one ref): %d bytes", len(strings.TrimSpace(string(raw))))
	for _, key := range []string{`"ownText"`, `"refs"`, `"fetchedAt"`, `"schemaVersion":"corpus.post.v1"`, `"sourceKeys":["core"]`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("stored line missing %s: %s", key, raw)
		}
	}
	if strings.Contains(string(raw), `"Text"`) {
		t.Errorf("stored line uses Go-name keys: %s", raw)
	}
}

// A retried run re-appends the same posts. The corpus must not count them twice, and
// the later snapshot (newer metrics) is the one to keep.
func TestLoadDedupesRetriedSaveKeepingTheLatestFetch(t *testing.T) {
	ctx := context.Background()
	store := &storage.FS{Root: t.TempDir()}
	first, retry := post("1900000000000000001", "a"), post("1900000000000000001", "a")
	first.Likes, retry.Likes = 10, 25
	if err := Save(ctx, store, RunRecord{FetchedAt: day2}, []StoredPost{first, post("1900000000000000002", "b")}); err != nil {
		t.Fatal(err)
	}
	later := day2.Add(time.Hour)
	if err := Save(ctx, store, RunRecord{FetchedAt: later}, []StoredPost{retry, post("1900000000000000002", "b")}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(ctx, store, day2, day2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d posts, want 2 (each once)", len(got))
	}
	if got[0].Likes != 25 || !got[0].FetchedAt.Equal(later) {
		t.Errorf("kept likes=%d fetchedAt=%v, want the later snapshot (25, %v)", got[0].Likes, got[0].FetchedAt, later)
	}
}

func TestSaveSplitsLargeDaysIntoChunksAndReadsAllBack(t *testing.T) {
	ctx := context.Background()
	store := newCountingStore(t)
	const n = 250
	posts := make([]StoredPost, n)
	for i := range posts {
		// about the size of a long-form post with a quoted reference
		posts[i] = post(fmt.Sprintf("19000000000000%05d", n-i), strings.Repeat("synthetic words ", 100))
	}
	if err := Save(ctx, store, RunRecord{FetchedAt: day2, Fetched: n}, posts); err != nil {
		t.Fatal(err)
	}
	sizes := store.appends[PostsKey(day2)]
	if len(sizes) < 2 {
		t.Fatalf("posts written in %d append(s), want more than one chunk", len(sizes))
	}
	for _, s := range sizes {
		if s > maxChunkBytes {
			t.Errorf("chunk of %d bytes exceeds the %d cap", s, maxChunkBytes)
		}
	}
	t.Logf("%d posts -> %d chunks, sizes %v bytes", n, len(sizes), sizes)
	got, err := Load(ctx, store, day2, day2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Fatalf("read back %d posts, want %d", len(got), n)
	}
	for i := 1; i < len(got); i++ {
		if !idLess(got[i-1].ID, got[i].ID) {
			t.Fatalf("posts not in id order at %d: %s then %s", i, got[i-1].ID, got[i].ID)
		}
	}
	runs, err := LoadRuns(ctx, store, day2, day2)
	if err != nil || len(runs) != 1 || runs[0].Written != n || runs[0].Fetched != n {
		t.Errorf("runs = %+v (err %v), want one record with Fetched=Written=%d", runs, err, n)
	}
}

// A store that fails partway must leave a run record saying the day is incomplete, so
// an export never mistakes a failed write for a quiet day.
func TestSavePartialFailureRecordsWrittenBelowFetched(t *testing.T) {
	ctx := context.Background()
	const n = 250
	posts := make([]StoredPost, n)
	for i := range posts {
		posts[i] = post(fmt.Sprintf("19000000000000%05d", i+1), strings.Repeat("synthetic words ", 100))
	}
	probe := newCountingStore(t)
	if err := Save(ctx, probe, RunRecord{FetchedAt: day2, Fetched: n}, posts); err != nil {
		t.Fatal(err)
	}
	chunks := len(probe.appends[PostsKey(day2)])
	if chunks < 3 {
		t.Fatalf("fixture makes %d chunks, need at least 3 to fail in the middle", chunks)
	}

	// first chunk lands, the second fails, then the store recovers for the rest
	store := newCountingStore(t, 2)
	err := Save(ctx, store, RunRecord{FetchedAt: day2, Fetched: n}, posts)
	if err == nil {
		t.Fatal("Save returned nil after a failed chunk")
	}
	runs, lerr := LoadRuns(ctx, store, day2, day2)
	if lerr != nil || len(runs) != 1 {
		t.Fatalf("runs = %+v (err %v), want one record despite the failure", runs, lerr)
	}
	if runs[0].Written >= runs[0].Fetched || runs[0].Written == 0 {
		t.Errorf("Written=%d Fetched=%d, want 0 < Written < Fetched", runs[0].Written, runs[0].Fetched)
	}
	got, _ := Load(ctx, store, day2, day2)
	if len(got) != runs[0].Written {
		t.Errorf("Load returned %d posts, run record says %d written", len(got), runs[0].Written)
	}
}

func TestLoadAcrossDaysSkipsAMissingDay(t *testing.T) {
	ctx := context.Background()
	store := &storage.FS{Root: t.TempDir()}
	if err := Save(ctx, store, RunRecord{FetchedAt: day3, Fetched: 1}, []StoredPost{post("10", "later day")}); err != nil {
		t.Fatal(err)
	}
	if err := Save(ctx, store, RunRecord{FetchedAt: day1, Fetched: 1}, []StoredPost{post("9", "first day")}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(ctx, store, day1, day3)
	if err != nil {
		t.Fatalf("a missing day must be empty, not an error: %v", err)
	}
	if len(got) != 2 || got[0].ID != "9" || got[1].ID != "10" {
		t.Errorf("got %+v, want ids 9 then 10 (numeric, not lexical, order)", got)
	}
	runs, err := LoadRuns(ctx, store, day1, day3)
	if err != nil || len(runs) != 2 || !runs[0].FetchedAt.Equal(day1) {
		t.Errorf("runs = %+v (err %v), want day1 then day3", runs, err)
	}
	if got, err := Load(ctx, store, day2, day2); err != nil || len(got) != 0 {
		t.Errorf("missing day alone = %v (err %v), want empty", got, err)
	}
	if _, err := Load(ctx, store, day3, day1); err == nil {
		t.Error("an inverted range must be an error, not silently empty")
	}
}

func TestLoadToleratesBlankLinesAndNamesTheKeyOnABadOne(t *testing.T) {
	ctx := context.Background()
	store := &storage.FS{Root: t.TempDir()}
	if err := store.Put(ctx, PostsKey(day2), []byte(`{"id":"1","fetchedAt":"2026-11-21T16:00:00Z"}`+"\n\n  \n")); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(ctx, store, day2, day2); err != nil || len(got) != 1 {
		t.Errorf("blank lines: got %v (err %v), want one post", got, err)
	}
	if err := store.Append(ctx, PostsKey(day2), []byte(`{"id":`)); err != nil {
		t.Fatal(err)
	}
	_, err := Load(ctx, store, day2, day2)
	if err == nil || !strings.Contains(err.Error(), PostsKey(day2)) {
		t.Errorf("err = %v, want a parse error naming %s", err, PostsKey(day2))
	}
}

func TestSaveRequiresFetchedAt(t *testing.T) {
	if err := Save(context.Background(), &storage.FS{Root: t.TempDir()}, RunRecord{}, nil); err == nil {
		t.Error("a run without FetchedAt has no partition; Save must refuse it")
	}
}

func TestStoreFromEnvUsesFilesystemWithoutDynamo(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOMATION_ROOT", root)
	t.Setenv("STORAGE_BACKEND", "")
	store, err := StoreFromEnv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fs, ok := store.(*storage.FS)
	if !ok || fs.Root != root {
		t.Errorf("store = %#v, want FS rooted at AUTOMATION_ROOT", store)
	}
}
