package knowledgefactory

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sl6117/automations/internal/corpus"
	"github.com/sl6117/automations/internal/kf"
	"github.com/sl6117/automations/internal/obs"
	"github.com/sl6117/automations/internal/runner"
	"github.com/sl6117/automations/internal/storage"
)

const sentinelToken = "github_pat_SENTINEL0123456789"

var (
	now   = time.Date(2026, 11, 21, 17, 30, 0, 0, time.UTC)
	today = time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
)

type fakeSink struct{ delivered []string }

func (f *fakeSink) Name() string { return "fake" }
func (f *fakeSink) Deliver(_ context.Context, msg string) error {
	f.delivered = append(f.delivered, msg)
	return nil
}

// forbiddenTransport fails the test on any HTTP request.
type forbiddenTransport struct{ t *testing.T }

func (f forbiddenTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Errorf("unexpected HTTP request: %s %s", r.Method, r.URL.Path)
	return nil, io.EOF
}

// projectDir writes a minimal project directory: config plus a synthetic universe.
func projectDir(t *testing.T, publish bool, universeVersion string) string {
	t.Helper()
	dir := t.TempDir()
	cfg := `{"publish": {"enabled": ` + map[bool]string{true: "true", false: "false"}[publish] +
		`, "repo": "example-owner/example-feed", "branch": "main", "pathPrefix": "desk-feed/v1"}}`
	universe := `{"version": "` + universeVersion + `", "entries": [{"ticker": "EXMPL", "type": "equity", "aliases": ["Example Corp"]}, {"ticker": "EXMPB", "type": "equity", "aliases": []}]}`
	for name, body := range map[string]string{"config.json": cfg, "universe.json": universe} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func seedDay(t *testing.T, store storage.Store, day time.Time, texts ...string) {
	t.Helper()
	var posts []corpus.StoredPost
	for i, text := range texts {
		id := "19000000000000" + day.Format("0102") + string(rune('0'+i))
		posts = append(posts, corpus.StoredPost{ID: id, Handle: "@example_wire", URL: "https://x.com/example_wire/status/" + id,
			Text: text, OwnText: text, SourceKeys: []string{"core"}})
	}
	run := corpus.RunRecord{FetchedAt: day.Add(16 * time.Hour), Source: "xapi", Reads: 50, Fetched: len(posts)}
	if err := corpus.Save(context.Background(), store, run, posts); err != nil {
		t.Fatal(err)
	}
}

type harness struct {
	p       *project
	corpus  *storage.FS
	main    *storage.FS
	alert   *fakeSink
	logs    bytes.Buffer
	rt      *runner.Runtime
	gh      *fakeGitHub
	tokenOK bool
}

func newHarness(t *testing.T, publish bool) *harness {
	t.Helper()
	t.Setenv("AUTOMATION_ROOT", t.TempDir())
	h := &harness{corpus: &storage.FS{Root: t.TempDir()}, main: &storage.FS{Root: t.TempDir()}, alert: &fakeSink{}}
	env := map[string]string{tokenEnv: sentinelToken}
	h.p = &project{
		store: h.main, corpus: h.corpus, now: func() time.Time { return now }, alert: h.alert,
		client: &http.Client{Transport: forbiddenTransport{t}},
		getenv: func(k string) string { return env[k] },
	}
	h.rt = &runner.Runtime{Log: log.New(&h.logs, "", 0), ProjectDir: projectDir(t, publish, "test-v1")}
	return h
}

func (h *harness) withGitHub(t *testing.T) *fakeGitHub {
	h.gh = newFakeGitHub(t)
	t.Cleanup(h.gh.server.Close)
	h.p.client = h.gh.server.Client()
	h.p.githubAPI = h.gh.server.URL
	return h.gh
}

// fakeGitHub implements the slice of the Contents API the publisher uses.
type fakeGitHub struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	files    map[string][]byte // repo path -> content
	puts     []string          // repo paths, in order
	bodies   [][]byte
	auth     []string
	raceOnce map[string]bool // paths whose first create loses a race to another writer
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	g := &fakeGitHub{t: t, files: map[string][]byte{}, raceOnce: map[string]bool{}}
	g.server = httptest.NewServer(http.HandlerFunc(g.handle))
	return g
}

func (g *fakeGitHub) handle(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.auth = append(g.auth, r.Header.Get("Authorization"))
	const prefix = "/repos/example-owner/example-feed/contents/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.Error(w, "unknown route", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, prefix)
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("ref") != "main" {
			g.t.Errorf("GET %s without ref=main", path)
		}
		data, ok := g.files[path]
		if !ok {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"type": "file", "path": path, "sha": blobSHA(data)})
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			g.t.Errorf("PUT %s: body is not JSON", path)
		}
		content, _ := base64.StdEncoding.DecodeString(req["content"].(string))
		if g.raceOnce[path] {
			delete(g.raceOnce, path)
			g.files[path] = content // another writer got there first with the same bytes
			http.Error(w, `{"message":"conflict"}`, http.StatusConflict)
			return
		}
		if _, exists := g.files[path]; exists {
			http.Error(w, `{"message":"sha wasn't supplied"}`, http.StatusUnprocessableEntity)
			return
		}
		g.puts = append(g.puts, path)
		g.bodies = append(g.bodies, body)
		g.files[path] = content
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"content":{}}`)
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	}
}

func storeFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(root, p)
			out[rel] = data
		}
		return nil
	})
	return out
}

func sameFiles(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !bytes.Equal(b[k], v) {
			return false
		}
	}
	return true
}

func TestRunSealsTodayAndLogsCostWithoutAnyHTTPWhenPublishingIsOff(t *testing.T) {
	h := newHarness(t, false)
	seedDay(t, h.corpus, today, "$EXMPL is up", "Example Corp again", "nothing")
	if err := h.p.Run(context.Background(), h.rt); err != nil {
		t.Fatalf("run: %v\n%s", err, h.logs.String())
	}
	if rev, _ := kf.Sealed(context.Background(), h.corpus, today); rev != 1 {
		t.Errorf("today sealed r%d, want r1", rev)
	}
	runs, err := obs.LoadRuns(context.Background(), h.main)
	if err != nil || len(runs) != 1 || runs[0].Project != projectName || runs[0].ItemCount != 2 ||
		runs[0].InputTokens != 0 || runs[0].OutputTokens != 0 || runs[0].CostUSD != 0 {
		t.Errorf("cost log = %+v (err %v), want one zero-token knowledge-factory run with 2 items", runs, err)
	}
	// `auto cost` must cope with a run that names no model
	var report bytes.Buffer
	if err := obs.Report(context.Background(), h.main, &report); err != nil || !strings.Contains(report.String(), "Runs: 1") {
		t.Errorf("cost report = %q (err %v)", report.String(), err)
	}
	if !strings.Contains(h.logs.String(), "2026-11-21: sealed r1 (mentions 2, entity_day 1, posts_fetched 3)") {
		t.Errorf("log missing the seal line:\n%s", h.logs.String())
	}
	if strings.Contains(h.logs.String(), "is up") || strings.Contains(h.logs.String(), "Example Corp") {
		t.Errorf("log carries post text:\n%s", h.logs.String())
	}
}

func TestNothingFetchedTodayIsNotAFailureAndCatchUpStillRuns(t *testing.T) {
	h := newHarness(t, false)
	seedDay(t, h.corpus, today.AddDate(0, 0, -1), "$EXMPL yesterday")
	if err := h.p.Run(context.Background(), h.rt); err != nil {
		t.Fatalf("run: %v", err)
	}
	if rev, _ := kf.Sealed(context.Background(), h.corpus, today.AddDate(0, 0, -1)); rev != 1 {
		t.Errorf("yesterday sealed r%d, want r1 from catch-up", rev)
	}
	if !strings.Contains(h.logs.String(), "nothing fetched on 2026-11-21") {
		t.Errorf("log does not say today had nothing:\n%s", h.logs.String())
	}
}

// Re-sealing a past day on every run would mint a revision per universe edit. Catch-up
// only seals a day that has no revision at all.
func TestCatchUpSealsOnlyUnsealedDays(t *testing.T) {
	h := newHarness(t, false)
	ctx := context.Background()
	sealedDay, openDay := today.AddDate(0, 0, -2), today.AddDate(0, 0, -1)
	seedDay(t, h.corpus, sealedDay, "$EXMPL two days ago", "$EXMPB too")
	seedDay(t, h.corpus, openDay, "$EXMPL yesterday")
	old, err := kf.NewUniverse("older-version", []kf.Entry{{Ticker: "EXMPL", Type: kf.TypeEquity}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kf.Export(ctx, h.corpus, old, sealedDay, func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	before := storeFiles(t, filepath.Join(h.corpus.Root, "exports"))

	if err := h.p.Run(ctx, h.rt); err != nil {
		t.Fatalf("run: %v", err)
	}
	if rev, _ := kf.Sealed(ctx, h.corpus, sealedDay); rev != 1 {
		t.Errorf("already sealed day now at r%d, want it left at r1 despite the new universe", rev)
	}
	after := storeFiles(t, filepath.Join(h.corpus.Root, "exports"))
	for k, v := range before {
		if !bytes.Equal(after[k], v) {
			t.Errorf("sealed file %s changed", k)
		}
	}
	if rev, _ := kf.Sealed(ctx, h.corpus, openDay); rev != 1 {
		t.Errorf("unsealed day at r%d, want r1", rev)
	}
}

func TestDryRunWritesNothingAndMakesNoRequest(t *testing.T) {
	h := newHarness(t, true) // publishing on, to prove a dry run still never reaches GitHub
	seedDay(t, h.corpus, today, "$EXMPL is up")
	seedDay(t, h.corpus, today.AddDate(0, 0, -1), "$EXMPL yesterday")
	h.rt.DryRun = true
	corpusBefore, mainBefore := storeFiles(t, h.corpus.Root), storeFiles(t, h.main.Root)
	if err := h.p.Run(context.Background(), h.rt); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !sameFiles(corpusBefore, storeFiles(t, h.corpus.Root)) || !sameFiles(mainBefore, storeFiles(t, h.main.Root)) {
		t.Error("dry run wrote to a store")
	}
	if !strings.Contains(h.logs.String(), "dry-run: 2026-11-21 would seal mentions 1, entity_day 1, posts_fetched 1") {
		t.Errorf("dry run log:\n%s", h.logs.String())
	}
	if len(h.alert.delivered) != 0 {
		t.Errorf("dry run alerted: %v", h.alert.delivered)
	}
}

// sealedFiles returns every sealed revision's files for the days a run publishes.
func sealedFiles(t *testing.T, store storage.Store) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for i := 0; i <= catchUpDays; i++ {
		day := today.AddDate(0, 0, -i)
		latest, err := kf.Sealed(context.Background(), store, day)
		if err != nil {
			t.Fatal(err)
		}
		for rev := 1; rev <= latest; rev++ {
			files, err := kf.RevisionFiles(context.Background(), store, day, rev)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range files {
				out["desk-feed/v1/"+f.Path] = f.Data
			}
		}
	}
	return out
}

func TestPublishCreatesExactlyTheSealedFilesInOrderThenNothing(t *testing.T) {
	h := newHarness(t, true)
	gh := h.withGitHub(t)
	seedDay(t, h.corpus, today, "$EXMPL is up")
	seedDay(t, h.corpus, today.AddDate(0, 0, -1), "no tickers") // an empty-mention day
	if err := h.p.Run(context.Background(), h.rt); err != nil {
		t.Fatalf("run: %v\n%s", err, h.logs.String())
	}
	want := sealedFiles(t, h.corpus)
	if len(gh.puts) != 6 || len(want) != 6 {
		t.Fatalf("created %v (sealed %d files), want 2 revisions x 3 files", gh.puts, len(want))
	}
	// per revision: both data files, then the manifest; older day first
	wantOrder := []string{
		"desk-feed/v1/mentions/2026-11-20.r1.jsonl", "desk-feed/v1/entity_day/2026-11-20.r1.jsonl", "desk-feed/v1/manifest/2026-11-20.r1.json",
		"desk-feed/v1/mentions/2026-11-21.r1.jsonl", "desk-feed/v1/entity_day/2026-11-21.r1.jsonl", "desk-feed/v1/manifest/2026-11-21.r1.json",
	}
	if strings.Join(gh.puts, ",") != strings.Join(wantOrder, ",") {
		t.Errorf("create order =\n%v\nwant\n%v", gh.puts, wantOrder)
	}
	// what leaves the machine is exactly the sealed files, and nothing else
	for i, body := range gh.bodies {
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatal(err)
		}
		keys := make([]string, 0, len(req))
		for k := range req {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if strings.Join(keys, ",") != "branch,content,message" {
			t.Errorf("PUT %s body keys = %v, want branch, content, message", gh.puts[i], keys)
		}
		content, err := base64.StdEncoding.DecodeString(req["content"].(string))
		if err != nil || !bytes.Equal(content, want[gh.puts[i]]) {
			t.Errorf("PUT %s content is not a byte-identical copy of the sealed file", gh.puts[i])
		}
		if msg := req["message"].(string); !strings.HasPrefix(msg, "desk-feed: 2026-11-") {
			t.Errorf("commit message %q", msg)
		}
	}
	if len(gh.files["desk-feed/v1/mentions/2026-11-20.r1.jsonl"]) != 0 {
		t.Error("the empty-mention day's data file was not published as an empty file")
	}
	for _, a := range gh.auth {
		if a != "Bearer "+sentinelToken {
			t.Fatalf("Authorization header = %q", a)
		}
	}

	gh.puts = nil
	if err := h.p.Run(context.Background(), h.rt); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(gh.puts) != 0 {
		t.Errorf("second run created %v, want nothing", gh.puts)
	}
}

func TestPublishSkipsAnIdenticalFileAndRetriesALostRace(t *testing.T) {
	h := newHarness(t, true)
	gh := h.withGitHub(t)
	seedDay(t, h.corpus, today, "$EXMPL is up")
	// seal first so the identical copy can be planted before publishing
	h.p.getenv = func(string) string { return "" }
	h.rt.ProjectDir = projectDir(t, false, "test-v1")
	if err := h.p.Run(context.Background(), h.rt); err != nil {
		t.Fatal(err)
	}
	files := sealedFiles(t, h.corpus)
	gh.files["desk-feed/v1/mentions/2026-11-21.r1.jsonl"] = files["desk-feed/v1/mentions/2026-11-21.r1.jsonl"]
	gh.raceOnce["desk-feed/v1/entity_day/2026-11-21.r1.jsonl"] = true

	h.p.getenv = func(k string) string { return map[string]string{tokenEnv: sentinelToken}[k] }
	h.rt.ProjectDir = projectDir(t, true, "test-v1")
	if err := h.p.Run(context.Background(), h.rt); err != nil {
		t.Fatalf("run: %v\n%s", err, h.logs.String())
	}
	if strings.Join(gh.puts, ",") != "desk-feed/v1/manifest/2026-11-21.r1.json" {
		t.Errorf("created %v, want only the manifest (mentions identical, entity_day won by the other writer)", gh.puts)
	}
	for path, want := range files {
		if !bytes.Equal(gh.files[path], want) {
			t.Errorf("%s in the repo differs from the sealed file", path)
		}
	}
}

func TestPublishFailureKeepsSealsErrorsAndAlertsOnceWithoutTheToken(t *testing.T) {
	h := newHarness(t, true)
	gh := h.withGitHub(t)
	seedDay(t, h.corpus, today, "$EXMPL is up")
	tampered := []byte("someone else's bytes " + sentinelToken + "\n")
	gh.files["desk-feed/v1/mentions/2026-11-21.r1.jsonl"] = tampered

	err := h.p.Run(context.Background(), h.rt)
	if err == nil || !strings.Contains(err.Error(), "different content") {
		t.Fatalf("err = %v, want a refusal to overwrite", err)
	}
	if !bytes.Equal(gh.files["desk-feed/v1/mentions/2026-11-21.r1.jsonl"], tampered) || len(gh.puts) != 0 {
		t.Errorf("the differing file was overwritten or other files were created: %v", gh.puts)
	}
	if rev, _ := kf.Sealed(context.Background(), h.corpus, today); rev != 1 {
		t.Errorf("seal lost after a publish failure (r%d)", rev)
	}
	if len(h.alert.delivered) != 1 {
		t.Errorf("alerts = %d, want exactly 1", len(h.alert.delivered))
	}
	for name, s := range map[string]string{"error": err.Error(), "log": h.logs.String(), "alert": strings.Join(h.alert.delivered, "\n")} {
		if strings.Contains(s, sentinelToken) {
			t.Errorf("%s contains the token", name)
		}
	}
	runs, _ := obs.LoadRuns(context.Background(), h.main)
	if len(runs) != 1 {
		t.Errorf("cost log has %d runs, want the run logged before publishing", len(runs))
	}
}

// The token can also come back in an error from the transport itself (a URL echo, a
// proxy message). It must be redacted there too.
func TestTokenIsRedactedFromTransportErrors(t *testing.T) {
	h := newHarness(t, true)
	seedDay(t, h.corpus, today, "$EXMPL is up")
	h.p.client = &http.Client{Transport: echoTokenTransport{}}
	h.p.githubAPI = "https://github.invalid"
	err := h.p.Run(context.Background(), h.rt)
	if err == nil {
		t.Fatal("run succeeded against a failing transport")
	}
	for name, s := range map[string]string{"error": err.Error(), "log": h.logs.String(), "alert": strings.Join(h.alert.delivered, "\n")} {
		if strings.Contains(s, sentinelToken) {
			t.Errorf("%s contains the token: %s", name, s)
		}
		if name != "log" && !strings.Contains(s, "***REDACTED***") {
			t.Errorf("%s does not show the redaction: %s", name, s)
		}
	}
}

type echoTokenTransport struct{}

func (echoTokenTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, &echoError{r.Header.Get("Authorization")}
}

type echoError struct{ auth string }

func (e *echoError) Error() string { return "proxy refused request with header " + e.auth }

func TestPublishingWithoutATokenNamesTheVariable(t *testing.T) {
	h := newHarness(t, true)
	h.p.getenv = func(string) string { return "" }
	seedDay(t, h.corpus, today, "$EXMPL is up")
	err := h.p.Run(context.Background(), h.rt)
	if err == nil || !strings.Contains(err.Error(), "KF_GITHUB_TOKEN is not set") {
		t.Errorf("err = %v, want it to name KF_GITHUB_TOKEN", err)
	}
	if len(h.alert.delivered) != 1 {
		t.Errorf("alerts = %d, want 1", len(h.alert.delivered))
	}
}

func TestShippedConfigKeepsPublishingOffAndUniverseLoads(t *testing.T) {
	var cfg Config
	data, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Publish.Enabled {
		t.Error("config.json enables publishing; it must ship off")
	}
	if err := cfg.Publish.validate(); err != nil {
		t.Errorf("shipped publish config is invalid: %v", err)
	}
	if _, err := kf.LoadUniverse("universe.json"); err != nil {
		t.Errorf("shipped universe: %v", err)
	}
}

func TestBlobSHAMatchesGit(t *testing.T) {
	// `git hash-object` of an empty file and of "hello\n"
	if got := blobSHA(nil); got != "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391" {
		t.Errorf("empty blob sha = %s", got)
	}
	if got := blobSHA([]byte("hello\n")); got != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Errorf("hello blob sha = %s", got)
	}
}
