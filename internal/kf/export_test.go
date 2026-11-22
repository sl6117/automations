package kf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/sl6117/automations/internal/corpus"
	"github.com/sl6117/automations/internal/storage"
)

var update = flag.Bool("update", false, "rewrite the desk-feed fixtures under docs/contracts/fixtures/desk-feed-v1")

const (
	contractsDir = "../../docs/contracts"
	fixturesDir  = contractsDir + "/fixtures/desk-feed-v1"
)

var (
	exportDay = time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	sealClock = func() time.Time { return time.Date(2026, 11, 21, 17, 0, 0, 0, time.UTC) }
	lateClock = func() time.Time { return time.Date(2026, 11, 21, 21, 0, 0, 0, time.UTC) }
)

func fixtureUniverse(t *testing.T) *Universe {
	t.Helper()
	u, err := NewUniverse("fixture-2026-11-21", []Entry{
		{Ticker: "EXMPL", Type: TypeEquity, Aliases: []string{"Example Corp"}},
		{Ticker: "EXMPB", Type: TypeEquity},
		{Ticker: "EXMPA.B", Type: TypeEquity},
		{Ticker: "EXETF", Type: TypeETF},
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// one id per account, so the fixtures show a consistent author_id for each handle
var fixtureAuthorIDs = map[string]string{
	"@example_wire": "1900000000000000901", "@example_desk": "1900000000000000902",
	"@example_fund": "1900000000000000903", "@example_relay": "1900000000000000904",
	"@example_hist": "1900000000000000905",
}

func fixturePost(id, handle, ownText string, likes int) corpus.StoredPost {
	return corpus.StoredPost{
		ID: id, Author: "Example Account", Handle: handle, Text: ownText, OwnText: ownText,
		URL:      "https://x.com/" + strings.TrimPrefix(handle, "@") + "/status/" + id,
		AuthorID: fixtureAuthorIDs[handle],
		Likes:    likes, Reposts: likes / 10, Replies: 2, Quotes: 1, Impressions: likes * 40, AuthorFollowers: 5000,
		CreatedAt: time.Date(2026, 11, 21, 15, 30, 0, 0, time.UTC), SourceKeys: []string{"core"},
	}
}

// seedHistory writes twelve prior days of ten posts each, with a varying count of $EXMPL
// mentions, so the fixture shows both a numeric and a null z_share.
func seedHistory(t *testing.T, store storage.Store) {
	t.Helper()
	counts := []int{1, 2, 0, 1, 3, 1, 2, 1, 0, 2, 1, 2}
	for i, k := range counts {
		day := exportDay.AddDate(0, 0, -(i + 1)).Add(16 * time.Hour)
		var posts []corpus.StoredPost
		for j := 0; j < 10; j++ {
			text := "quiet day"
			if j < k {
				text = "watching $EXMPL"
			}
			posts = append(posts, fixturePost(fmt.Sprintf("18%02d00000000000%03d", i+1, j), "@example_hist", text, 10))
		}
		if err := corpus.Save(context.Background(), store, corpus.RunRecord{FetchedAt: day, Source: "xapi", Reads: 50, Fetched: 10}, posts); err != nil {
			t.Fatal(err)
		}
	}
}

func seedFixtureDay(t *testing.T, store storage.Store) {
	t.Helper()
	cash := fixturePost("1900000000000000001", "@example_wire", "$EXMPL margins look strong https://t.co/example", 400)
	rt := fixturePost("1900000000000000002", "@example_relay", "", 0)
	rt.Text = "RT @example_desk: $EXMPB guidance raised"
	rt.Reposts = 80
	rt.Refs = []corpus.Ref{{Type: "retweeted", ID: "1900000000000000012", AuthorID: "1900000000000000902", AuthorHandle: "@example_desk", Text: "$EXMPB guidance raised"}}
	quote := fixturePost("1900000000000000003", "@example_wire", "Example Corp again", 150)
	quote.Refs = []corpus.Ref{{Type: "quoted", ID: "1900000000000000013", AuthorID: "1900000000000000903", AuthorHandle: "@example_fund", Text: "$EXMPL and $NOPE into earnings"}}
	quote.Text = quote.OwnText + "\n[quoting @example_fund: $EXMPL and $NOPE into earnings]"
	plain := fixturePost("1900000000000000004", "@example_desk", "no tickers in this one", 90)
	run := corpus.RunRecord{FetchedAt: time.Date(2026, 11, 21, 16, 0, 0, 500_000_000, time.UTC), Source: "xapi", Reads: 50, Fetched: 4}
	if err := corpus.Save(context.Background(), store, run, []corpus.StoredPost{cash, rt, quote, plain}); err != nil {
		t.Fatal(err)
	}
}

// addLateFetch is a second, later fetch that day which hit the page cap.
func addLateFetch(t *testing.T, store storage.Store) {
	t.Helper()
	late := fixturePost("1900000000000000005", "@example_desk", "$EXMPA.B listed today", 60)
	late.CreatedAt = time.Time{} // age unknown: created_at and age_at_fetch_s are omitted
	run := corpus.RunRecord{FetchedAt: time.Date(2026, 11, 21, 20, 0, 0, 0, time.UTC), Source: "xapi", Reads: 250, Truncated: true, Fetched: 1}
	if err := corpus.Save(context.Background(), store, run, []corpus.StoredPost{late}); err != nil {
		t.Fatal(err)
	}
}

// exportedFiles returns every file under exports/ in an FS store, keyed by store key.
func exportedFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	base := filepath.Join(root, "exports")
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return out
}

func newFixtureStore(t *testing.T) (*storage.FS, string) {
	root := t.TempDir()
	store := &storage.FS{Root: root}
	seedHistory(t, store)
	seedFixtureDay(t, store)
	return store, root
}

func TestExportRerunIsUnchanged(t *testing.T) {
	store, root := newFixtureStore(t)
	u := fixtureUniverse(t)
	res, err := Export(context.Background(), store, u, exportDay, sealClock)
	if err != nil || res.Outcome != OutcomeSealed || res.Revision != 1 || res.Supersedes != 0 {
		t.Fatalf("first export = %+v (err %v), want sealed r1", res, err)
	}
	before := exportedFiles(t, root)
	if len(before) != 3 {
		t.Fatalf("first export wrote %d files, want 3", len(before))
	}
	res, err = Export(context.Background(), store, u, exportDay, lateClock)
	if err != nil || res.Outcome != OutcomeUnchanged || res.Revision != 1 {
		t.Fatalf("rerun = %+v (err %v), want unchanged r1", res, err)
	}
	if after := exportedFiles(t, root); !equalFiles(before, after) {
		t.Errorf("an unchanged rerun touched the export: before %v after %v", keys(before), keys(after))
	}
}

func TestLateFetchSealsR2AndLeavesR1Untouched(t *testing.T) {
	store, root := newFixtureStore(t)
	u := fixtureUniverse(t)
	if _, err := Export(context.Background(), store, u, exportDay, sealClock); err != nil {
		t.Fatal(err)
	}
	r1 := exportedFiles(t, root)
	addLateFetch(t, store)
	res, err := Export(context.Background(), store, u, exportDay, lateClock)
	if err != nil || res.Outcome != OutcomeSealed || res.Revision != 2 || res.Supersedes != 1 {
		t.Fatalf("export after a late fetch = %+v (err %v), want sealed r2 superseding r1", res, err)
	}
	after := exportedFiles(t, root)
	for k, v := range r1 {
		if !bytes.Equal(after[k], v) {
			t.Errorf("sealed r1 file %s changed", k)
		}
	}
	if len(after) != 6 {
		t.Errorf("files = %v, want r1 and r2 trios", keys(after))
	}
	var m Manifest
	if err := json.Unmarshal(after["exports/v1/manifest/2026-11-21.r2.json"], &m); err != nil {
		t.Fatal(err)
	}
	if m.Supersedes == nil || *m.Supersedes != 1 || !m.WindowTruncated || m.SealedAt != "2026-11-21T21:00:00Z" {
		t.Errorf("r2 manifest = %+v, want supersedes 1, truncated, sealed_at from the clock", m)
	}
}

// A crash between the data files and the manifest leaves r1 unsealed; the next run must
// treat it as absent and seal r1 properly rather than skipping to r2.
func TestUnsealedLeftoversAreResealedAsR1(t *testing.T) {
	store, root := newFixtureStore(t)
	ctx := context.Background()
	mPath, ePath, _ := RelPaths("2026-11-21", 1)
	if err := store.Put(ctx, exportPrefix+mPath, []byte("half written")); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, exportPrefix+ePath, []byte("{")); err != nil {
		t.Fatal(err)
	}
	res, err := Export(ctx, store, fixtureUniverse(t), exportDay, sealClock)
	if err != nil || res.Outcome != OutcomeSealed || res.Revision != 1 {
		t.Fatalf("export = %+v (err %v), want sealed r1", res, err)
	}
	files := exportedFiles(t, root)
	var m Manifest
	if err := json.Unmarshal(files["exports/v1/"+"manifest/2026-11-21.r1.json"], &m); err != nil {
		t.Fatal(err)
	}
	for _, f := range m.Files {
		if sha256Hex(files[exportPrefix+f.Path]) != f.SHA256 {
			t.Errorf("%s still holds the crash leftovers", f.Path)
		}
	}
}

func TestExportRefusesDaysWithNothingToSay(t *testing.T) {
	ctx := context.Background()
	u := fixtureUniverse(t)

	root := t.TempDir()
	store := &storage.FS{Root: root}
	// posts without a run record: not a fetched day
	if err := store.Append(ctx, corpus.PostsKey(exportDay), []byte(`{"id":"1","ownText":"$EXMPL","fetchedAt":"2026-11-21T16:00:00Z"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := Export(ctx, store, u, exportDay, sealClock); !errors.Is(err, ErrNothingFetched) || !strings.Contains(err.Error(), "2026-11-21") {
		t.Errorf("err = %v, want nothing fetched on 2026-11-21", err)
	}
	if files := exportedFiles(t, root); len(files) != 0 {
		t.Errorf("refused export still wrote %v", keys(files))
	}

	future := exportDay.AddDate(0, 0, 1)
	if _, err := Export(ctx, store, u, future, sealClock); err == nil || !strings.Contains(err.Error(), "future") {
		t.Errorf("err = %v, want a refusal for a future date", err)
	}
	if files := exportedFiles(t, root); len(files) != 0 {
		t.Errorf("refused export still wrote %v", keys(files))
	}
}

func TestEmptyMentionDayStillSeals(t *testing.T) {
	root := t.TempDir()
	store := &storage.FS{Root: root}
	posts := []corpus.StoredPost{fixturePost("1900000000000000001", "@example_wire", "nothing to see", 10)}
	if err := corpus.Save(context.Background(), store, corpus.RunRecord{FetchedAt: exportDay.Add(16 * time.Hour), Fetched: 1}, posts); err != nil {
		t.Fatal(err)
	}
	res, err := Export(context.Background(), store, fixtureUniverse(t), exportDay, sealClock)
	if err != nil || res.Outcome != OutcomeSealed || res.Mentions != 0 || res.EntityDays != 0 || res.PostsFetched != 1 {
		t.Fatalf("export = %+v (err %v), want sealed with zero records and 1 post fetched", res, err)
	}
	files := exportedFiles(t, root)
	m1, e1, man := RelPaths("2026-11-21", 1)
	if d, ok := files[exportPrefix+m1]; !ok || len(d) != 0 {
		t.Errorf("mentions file present=%v len=%d, want present and empty", ok, len(d))
	}
	if d, ok := files[exportPrefix+e1]; !ok || len(d) != 0 {
		t.Errorf("entity_day file present=%v len=%d, want present and empty", ok, len(d))
	}
	var m Manifest
	if err := json.Unmarshal(files[exportPrefix+man], &m); err != nil {
		t.Fatal(err)
	}
	if m.Files[0].Records != 0 || m.Files[1].Records != 0 || m.PostsFetched != 1 || m.UnmappedCashtags == nil {
		t.Errorf("manifest = %+v, want zero record counts, posts_fetched 1, unmapped []", m)
	}
	validateFile(t, "manifest", files[exportPrefix+man])
}

// Nothing but contract fields may leave: no post text, no display names, nothing the
// digest knows about subscribers.
func TestExportLeaksNothingOutsideTheContract(t *testing.T) {
	root := t.TempDir()
	store := &storage.FS{Root: root}
	ctx := context.Background()
	sentinels := []string{"SENTINEL_TEXT", "SENTINEL_OWN", "SENTINEL_REF", "SENTINEL_NAME", "sentinel-subscriber@example.com"}
	p := fixturePost("1900000000000000001", "@example_wire", "SENTINEL_OWN $EXMPL and Example Corp", 100)
	p.Text = "SENTINEL_TEXT " + p.OwnText
	p.Author = "SENTINEL_NAME"
	p.SourceKeys = []string{"sentinel-subscriber@example.com"}
	p.DropReason = "sentinel-subscriber@example.com"
	p.Refs = []corpus.Ref{{Type: "quoted", ID: "1900000000000000011", AuthorID: "1900000000000000902", AuthorHandle: "@example_desk", Text: "SENTINEL_REF $EXMPB $NOPE"}}
	if err := corpus.Save(ctx, store, corpus.RunRecord{FetchedAt: exportDay.Add(16 * time.Hour), Fetched: 1}, []corpus.StoredPost{p}); err != nil {
		t.Fatal(err)
	}
	if _, err := Export(ctx, store, fixtureUniverse(t), exportDay, sealClock); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if _, _, err := Materialize(ctx, store, exportDay, out); err != nil {
		t.Fatal(err)
	}

	all := exportedFiles(t, root)
	_ = filepath.WalkDir(out, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, _ := os.ReadFile(path)
			all["out:"+path] = data
		}
		return nil
	})
	if len(all) != 6 {
		t.Fatalf("exported files = %v, want 3 in the store and 3 in --out", keys(all))
	}
	for name, data := range all {
		for _, s := range sentinels {
			if bytes.Contains(data, []byte(s)) {
				t.Errorf("%s contains %q", name, s)
			}
		}
	}

	m1, e1, man := RelPaths("2026-11-21", 1)
	checkKeys(t, "mention", all[exportPrefix+m1], true)
	checkKeys(t, "entity_day", all[exportPrefix+e1], true)
	checkKeys(t, "manifest", all[exportPrefix+man], true)

	// --out holds the same bytes, and they match the manifest
	var m Manifest
	if err := json.Unmarshal(all[exportPrefix+man], &m); err != nil {
		t.Fatal(err)
	}
	for _, f := range m.Files {
		got, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(f.Path)))
		if err != nil || sha256Hex(got) != f.SHA256 || f.Records != bytes.Count(got, []byte("\n")) {
			t.Errorf("--out %s: sha/records do not match the manifest (err %v)", f.Path, err)
		}
	}
}

func TestMaterializeRefusesATamperedFile(t *testing.T) {
	store, _ := newFixtureStore(t)
	ctx := context.Background()
	if _, err := Export(ctx, store, fixtureUniverse(t), exportDay, sealClock); err != nil {
		t.Fatal(err)
	}
	m1, _, _ := RelPaths("2026-11-21", 1)
	if err := store.Put(ctx, exportPrefix+m1, []byte("tampered\n")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Materialize(ctx, store, exportDay, t.TempDir()); err == nil {
		t.Error("materialized a file whose sha256 no longer matches its manifest")
	}
}

// TestDeskFeedFixtures pins the checked-in contract fixtures: one sealed day (r1) and a
// second revision after a late fetch (r2). Regenerate after an intended change with
//
//	go test ./internal/kf -run TestDeskFeedFixtures -update
func TestDeskFeedFixtures(t *testing.T) {
	store, root := newFixtureStore(t)
	u := fixtureUniverse(t)
	if _, err := Export(context.Background(), store, u, exportDay, sealClock); err != nil {
		t.Fatal(err)
	}
	addLateFetch(t, store)
	if _, err := Export(context.Background(), store, u, exportDay, lateClock); err != nil {
		t.Fatal(err)
	}
	files := exportedFiles(t, root)
	if len(files) != 6 {
		t.Fatalf("exported %v, want two revisions of three files", keys(files))
	}
	for key, data := range files {
		rel := strings.TrimPrefix(key, exportPrefix)
		path := filepath.Join(fixturesDir, filepath.FromSlash(rel))
		if *update {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("missing fixture %s (regenerate with -update): %v", rel, err)
		}
		if !bytes.Equal(data, want) {
			t.Errorf("fixture %s differs from a fresh export; if intended, regenerate with -update\ngot:\n%s", rel, data)
		}
	}
}

// Every checked-in fixture record validates against its published schema.
func TestFixturesValidateAgainstSchemas(t *testing.T) {
	n := 0
	err := filepath.WalkDir(fixturesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		kind := filepath.Base(filepath.Dir(path))
		validateFile(t, kind, data)
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 6 {
		t.Errorf("validated %d fixture files, want 6", n)
	}
}

func schemaFor(t *testing.T, kind string) (*jsonschema.Resolved, map[string]any) {
	t.Helper()
	name := map[string]string{"mentions": "kf.mention.v1", "mention": "kf.mention.v1", "entity_day": "kf.entity_day.v1", "manifest": "kf.manifest.v1"}[kind]
	data, err := os.ReadFile(filepath.Join(contractsDir, name+".schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	resolved, err := s.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	return resolved, raw
}

// records splits a data file into its lines, or returns the manifest as one record.
func records(t *testing.T, kind string, data []byte) []map[string]any {
	t.Helper()
	var chunks [][]byte
	if kind == "manifest" {
		chunks = [][]byte{data}
	} else {
		for _, line := range bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n")) {
			if len(line) > 0 {
				chunks = append(chunks, line)
			}
		}
	}
	var out []map[string]any
	for _, c := range chunks {
		var m map[string]any
		if err := json.Unmarshal(c, &m); err != nil {
			t.Fatalf("%s record does not parse: %v\n%s", kind, err, c)
		}
		out = append(out, m)
	}
	return out
}

func validateFile(t *testing.T, kind string, data []byte) {
	t.Helper()
	resolved, _ := schemaFor(t, kind)
	for _, r := range records(t, kind, data) {
		if err := resolved.Validate(r); err != nil {
			t.Errorf("%s record fails its schema: %v\n%v", kind, err, r)
		}
	}
}

// checkKeys asserts every object in each record has only schema properties and, when
// exact is set (a fully populated fixture), exactly the schema's property set.
func checkKeys(t *testing.T, kind string, data []byte, exact bool) {
	t.Helper()
	_, schema := schemaFor(t, kind)
	recs := records(t, kind, data)
	if len(recs) == 0 {
		t.Fatalf("%s: no records to check", kind)
	}
	for _, r := range recs {
		compareKeys(t, kind, r, schema, exact)
	}
}

func compareKeys(t *testing.T, where string, obj map[string]any, schema map[string]any, exact bool) {
	t.Helper()
	props, _ := schema["properties"].(map[string]any)
	var want []string
	for k := range props {
		want = append(want, k)
	}
	var got []string
	for k := range obj {
		got = append(got, k)
	}
	sort.Strings(want)
	sort.Strings(got)
	for _, k := range got {
		if _, ok := props[k]; !ok {
			t.Errorf("%s: key %q is not in the schema", where, k)
		}
	}
	if exact && strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s: keys %v, want exactly %v", where, got, want)
	}
	for k, v := range obj {
		sub, _ := props[k].(map[string]any)
		switch val := v.(type) {
		case map[string]any:
			compareKeys(t, where+"."+k, val, sub, exact)
		case []any:
			items, _ := sub["items"].(map[string]any)
			for _, item := range val {
				if m, ok := item.(map[string]any); ok {
					compareKeys(t, where+"."+k+"[]", m, items, exact)
				}
			}
		}
	}
}

func equalFiles(a, b map[string][]byte) bool {
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

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
