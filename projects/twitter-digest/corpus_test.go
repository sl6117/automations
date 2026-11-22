package twitterdigest

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/internal/config"
	"github.com/sl6117/automations/internal/corpus"
	"github.com/sl6117/automations/internal/runner"
	"github.com/sl6117/automations/internal/storage"
	"github.com/sl6117/automations/pkg/sinks"
	"github.com/sl6117/automations/pkg/sources"
)

// corpusSource returns one post that clears the filter and one that does not.
type corpusSource struct{ tweets []sources.Tweet }

func (corpusSource) Name() string { return "corpus-fixture" }

func (c corpusSource) Fetch(ctx context.Context) ([]sources.Tweet, error) { return c.tweets, nil }

var corpusFixture = []sources.Tweet{
	{ID: "1900000000000000002", Author: "Example Wire", Handle: "@example_wire",
		Text: "Strong quarter for $EXMPL", OwnText: "Strong quarter for $EXMPL",
		URL: "https://x.com/example_wire/status/1900000000000000002", Likes: 500, Reposts: 50, AuthorID: "1900000000000000901"},
	{ID: "1900000000000000001", Author: "Example Desk", Handle: "@example_desk",
		Text: "Quiet note on $EXMPL nobody liked", OwnText: "Quiet note on $EXMPL nobody liked",
		URL: "https://x.com/example_desk/status/1900000000000000001", Likes: 1, Reposts: 0, AuthorID: "1900000000000000902"},
}

// forbiddenStore fails the test on any use: proof a code path never touches the corpus.
type forbiddenStore struct{ t *testing.T }

func (f forbiddenStore) Get(context.Context, string) ([]byte, error) {
	f.t.Error("corpus store read")
	return nil, storage.ErrNotExist
}
func (f forbiddenStore) Put(context.Context, string, []byte) error {
	f.t.Error("corpus store Put")
	return nil
}
func (f forbiddenStore) Append(context.Context, string, []byte) error {
	f.t.Error("corpus store Append")
	return nil
}
func (f forbiddenStore) List(context.Context, string) ([]string, error) {
	f.t.Error("corpus store List")
	return nil, nil
}

// brokenStore fails every write, like an unreachable or throttled table.
type brokenStore struct{ storage.Store }

func (brokenStore) Append(context.Context, string, []byte) error {
	return errors.New("synthetic corpus outage")
}

// corpusProjectDir writes a minimal project dir with the corpus flag set as given.
func corpusProjectDir(t *testing.T, root string, enabled bool) string {
	t.Helper()
	dir := filepath.Join(root, "projects", "twitter-digest")
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	flag := "false"
	if enabled {
		flag = "true"
	}
	files := map[string]string{
		"config.json":       `{"topics":[{"name":"Stocks"}],"source":"mock","model":"m","minEngagement":100,"corpus":{"enabled":` + flag + `}}`,
		"prompts/digest.md": "write a digest\n{{TOPICS}}\n{{TWEETS_JSON}}",
		"prompts/judge.md":  "quality evaluator\n{{TOPICS}}\n{{TWEETS_JSON}}\n{{DIGEST}}",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

type corpusRun struct {
	root   string
	store  storage.Store
	sink   *fakeSink
	log    bytes.Buffer
	before time.Time
}

func runWithCorpus(t *testing.T, enabled, dryRun bool, tweets []sources.Tweet, corpusStore storage.Store) *corpusRun {
	t.Helper()
	root := t.TempDir()
	t.Setenv("AUTOMATION_ROOT", root)
	t.Setenv("JEV_API_KEY", "")
	t.Setenv("STORAGE_BACKEND", "")
	r := &corpusRun{root: root, store: &storage.FS{Root: root}, sink: &fakeSink{}, before: time.Now().UTC()}
	p := &project{
		client: &fakeClient{resp: ai.Response{Text: "## Stocks\n- strong quarter (@example_wire https://x.com/example_wire/status/1900000000000000002)"}},
		source: corpusSource{tweets: tweets},
		sinks:  []sinks.Sink{r.sink},
		store:  r.store,
		corpus: corpusStore,
		alert:  &fakeSink{},
	}
	runTime := &runner.Runtime{DryRun: dryRun, Log: log.New(&r.log, "", 0), ProjectDir: corpusProjectDir(t, root, enabled)}
	if err := p.Run(context.Background(), runTime); err != nil {
		t.Fatalf("run failed: %v\n%s", err, r.log.String())
	}
	return r
}

func loadToday(t *testing.T, store storage.Store) ([]corpus.StoredPost, []corpus.RunRecord) {
	t.Helper()
	// a run can straddle UTC midnight, so read yesterday through today
	now := time.Now().UTC()
	posts, err := corpus.Load(context.Background(), store, now.AddDate(0, 0, -1), now)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := corpus.LoadRuns(context.Background(), store, now.AddDate(0, 0, -1), now)
	if err != nil {
		t.Fatal(err)
	}
	return posts, runs
}

func TestCorpusStoresKeptAndDroppedPosts(t *testing.T) {
	cs := &storage.FS{Root: t.TempDir()}
	r := runWithCorpus(t, true, false, corpusFixture, cs)
	posts, runs := loadToday(t, cs)
	if len(posts) != 2 {
		t.Fatalf("stored %d posts, want both the kept and the dropped one", len(posts))
	}
	dropped, kept := posts[0], posts[1] // id order
	if dropped.Kept || dropped.DropReason != string(DropLowEngagement) || dropped.Text != corpusFixture[1].Text {
		t.Errorf("dropped post = %+v, want full text and drop reason %s", dropped, DropLowEngagement)
	}
	if !kept.Kept || kept.DropReason != "" || kept.OwnText != corpusFixture[0].OwnText || kept.AuthorID != corpusFixture[0].AuthorID {
		t.Errorf("kept post = %+v, want Kept with its structured fields", kept)
	}
	if len(kept.SourceKeys) != 1 || kept.SourceKeys[0] != "core" || kept.FetchedAt.Before(r.before) {
		t.Errorf("kept post sourceKeys=%v fetchedAt=%v, want [core] and this run's fetch time", kept.SourceKeys, kept.FetchedAt)
	}
	if len(runs) != 1 || runs[0].Fetched != 2 || runs[0].Written != 2 || runs[0].Source != "corpus-fixture" {
		t.Errorf("runs = %+v, want one record with Fetched=Written=2", runs)
	}
	if len(r.sink.delivered) != 1 {
		t.Errorf("delivered %d messages, want 1", len(r.sink.delivered))
	}
}

// Quiet days are the denominator's low end: a day with nothing kept must still be stored.
func TestCorpusRecordsAQuietDay(t *testing.T) {
	cs := &storage.FS{Root: t.TempDir()}
	r := runWithCorpus(t, true, false, corpusFixture[1:], cs)
	posts, runs := loadToday(t, cs)
	if len(posts) != 1 || posts[0].Kept {
		t.Errorf("posts = %+v, want the one dropped post", posts)
	}
	if len(runs) != 1 || runs[0].Fetched != 1 || runs[0].Written != 1 {
		t.Errorf("runs = %+v, want one record for the quiet run", runs)
	}
	if len(r.sink.delivered) != 0 {
		t.Errorf("delivered %d messages on a quiet day, want 0", len(r.sink.delivered))
	}
}

func TestCorpusWriteFailureNeverBlocksDelivery(t *testing.T) {
	r := runWithCorpus(t, true, false, corpusFixture, brokenStore{Store: &storage.FS{Root: t.TempDir()}})
	if len(r.sink.delivered) != 1 {
		t.Errorf("delivered %d messages, want 1: a corpus outage must not cost the digest", len(r.sink.delivered))
	}
	if !strings.Contains(r.log.String(), "corpus write failed") {
		t.Errorf("corpus failure not logged:\n%s", r.log.String())
	}
	state, err := loadState(context.Background(), r.store)
	if err != nil || state.SinceID != "1900000000000000002" {
		t.Errorf("cursor = %q (err %v), want it advanced as on any delivered run", state.SinceID, err)
	}
}

func TestCorpusFlagOffNeverTouchesTheStore(t *testing.T) {
	r := runWithCorpus(t, false, false, corpusFixture, forbiddenStore{t})
	if len(r.sink.delivered) != 1 {
		t.Errorf("delivered %d messages, want 1", len(r.sink.delivered))
	}
	// with no injected store, the env-selected one must not even be resolved
	r = runWithCorpus(t, false, false, corpusFixture, nil)
	if _, err := os.Stat(filepath.Join(r.root, "corpus")); !os.IsNotExist(err) {
		t.Errorf("corpus dir exists with the flag off (stat err %v)", err)
	}
	if strings.Contains(r.log.String(), "corpus") {
		t.Errorf("flag off but the run logged corpus activity:\n%s", r.log.String())
	}
}

func TestCorpusDryRunWritesNothing(t *testing.T) {
	runWithCorpus(t, true, true, corpusFixture, forbiddenStore{t})
}

// The flag ships dark: turning it on is the owner's call after deploy.
func TestShippedConfigKeepsCorpusOff(t *testing.T) {
	var cfg Config
	if err := config.Load("config.json", &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Corpus.Enabled {
		t.Error("config.json enables the corpus; it must stay off in this change")
	}
}
