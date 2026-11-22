package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sl6117/automations/internal/corpus"
	"github.com/sl6117/automations/internal/kf"
	"github.com/sl6117/automations/internal/storage"
)

func kfFixture(t *testing.T) (storage.Store, string) {
	t.Helper()
	t.Setenv("AUTOMATION_ROOT", t.TempDir())
	store := &storage.FS{Root: t.TempDir()}
	post := corpus.StoredPost{ID: "1900000000000000001", Handle: "@example_wire", URL: "https://x.com/example_wire/status/1900000000000000001",
		Text: "secret words $EXMPL", OwnText: "secret words $EXMPL", SourceKeys: []string{"core"}}
	run := corpus.RunRecord{FetchedAt: time.Date(2026, 11, 21, 16, 0, 0, 0, time.UTC), Source: "xapi", Fetched: 1}
	if err := corpus.Save(context.Background(), store, run, []corpus.StoredPost{post}); err != nil {
		t.Fatal(err)
	}
	universe := filepath.Join(t.TempDir(), "universe.json")
	if err := os.WriteFile(universe, []byte(`{"version":"test","entries":[{"ticker":"EXMPL","type":"equity","aliases":[]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return store, universe
}

// noEnv is an empty environment: the tests never read the ambient STORAGE_BACKEND.
func noEnv(string) string { return "" }

func dynamoEnv(key string) string {
	if key == "STORAGE_BACKEND" {
		return "dynamo"
	}
	return ""
}

func opts(store storage.Store, universe string, clock time.Time) kfExportOptions {
	return kfExportOptions{
		Date: "2026-11-21", UniversePath: universe, Getenv: noEnv,
		OpenStore: func(context.Context) (storage.Store, error) { return store, nil },
		Now:       func() time.Time { return clock },
	}
}

func TestKFExportRefusesDynamoWithoutTheFlag(t *testing.T) {
	_, universe := kfFixture(t)
	// The dynamo setting is injected through Getenv rather than t.Setenv: tests never set
	// STORAGE_BACKEND=dynamo in the real environment. The store constructor fails the test.
	o := opts(nil, universe, time.Now())
	o.Getenv = dynamoEnv
	o.OpenStore = func(context.Context) (storage.Store, error) {
		t.Error("a store was constructed despite the dynamo guard")
		return nil, nil
	}
	var out bytes.Buffer
	err := kfExport(context.Background(), &out, o)
	if err == nil || !strings.Contains(err.Error(), "--allow-dynamo") {
		t.Errorf("err = %v, want a refusal naming --allow-dynamo", err)
	}
}

func TestKFExportAllowDynamoFlagPassesTheGuard(t *testing.T) {
	store, universe := kfFixture(t)
	o := opts(store, universe, time.Date(2026, 11, 21, 17, 0, 0, 0, time.UTC))
	o.Getenv = dynamoEnv
	o.AllowDynamo = true
	var out bytes.Buffer
	if err := kfExport(context.Background(), &out, o); err != nil {
		t.Fatalf("with --allow-dynamo the guard must let the (injected) store through: %v", err)
	}
}

func TestKFExportSealsReportsAndMaterializes(t *testing.T) {
	store, universe := kfFixture(t)
	dir := t.TempDir()
	o := opts(store, universe, time.Date(2026, 11, 21, 17, 0, 0, 0, time.UTC))
	o.Out = dir

	var out bytes.Buffer
	if err := kfExport(context.Background(), &out, o); err != nil {
		t.Fatal(err)
	}
	o.Now = func() time.Time { return time.Date(2026, 11, 21, 18, 0, 0, 0, time.UTC) }
	if err := kfExport(context.Background(), &out, o); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"2026-11-21: sealed r1 (mentions 1, entity_day 1, posts_fetched 1)",
		"2026-11-21: unchanged r1 (mentions 1, entity_day 1, posts_fetched 1)",
		"wrote r1 to " + dir + ": 3 files",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "secret words") {
		t.Errorf("output printed post text:\n%s", got)
	}

	_, _, man := kf.RelPaths("2026-11-21", 1)
	data, err := os.ReadFile(filepath.Join(dir, man))
	if err != nil {
		t.Fatal(err)
	}
	var m kf.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, f := range m.Files {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
		sum := sha256.Sum256(b)
		if err != nil || hex.EncodeToString(sum[:]) != f.SHA256 {
			t.Errorf("--out %s does not match the manifest sha256 (err %v)", f.Path, err)
		}
	}
}

func TestKFExportRejectsABadDate(t *testing.T) {
	store, universe := kfFixture(t)
	o := opts(store, universe, time.Now())
	o.Date = "21/11/2026"
	if err := kfExport(context.Background(), &bytes.Buffer{}, o); err == nil {
		t.Error("accepted a date that is not YYYY-MM-DD")
	}
}
