package storage

import (
	"context"
	"errors"
	"testing"
)

func TestFSGetMissingReturnsErrNotExist(t *testing.T) {
	fs := &FS{Root: t.TempDir()}
	_, err := fs.Get(context.Background(), "nope/missing.json")
	if !errors.Is(err, ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
}

func TestFSPutThenGetRoundTrips(t *testing.T) {
	fs := &FS{Root: t.TempDir()}
	ctx := context.Background()
	if err := fs.Put(ctx, "a/b/state.json", []byte(`{"x":1}`)); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := fs.Get(ctx, "a/b/state.json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != `{"x":1}` {
		t.Fatalf("round trip mismatch: %q", got)
	}
}

func TestFSAppendAccumulatesLines(t *testing.T) {
	fs := &FS{Root: t.TempDir()}
	ctx := context.Background()
	if err := fs.Append(ctx, "logs/x.jsonl", []byte(`{"n":1}`)); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if err := fs.Append(ctx, "logs/x.jsonl", []byte(`{"n":2}`)); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	got, err := fs.Get(ctx, "logs/x.jsonl")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "{\"n\":1}\n{\"n\":2}\n" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestFSListReturnsSortedKeysUnderPrefixOnly(t *testing.T) {
	ctx := context.Background()
	f := &FS{Root: t.TempDir()}

	for _, key := range []string{
		"logs/runs/2026-07-09-b.json",
		"logs/runs/2026-07-07-a.json",
		"logs/cost-log.jsonl",
		"projects/twitter-digest/state.json",
	} {
		if err := f.Put(ctx, key, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	got, err := f.List(ctx, "logs/runs/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []string{"logs/runs/2026-07-07-a.json", "logs/runs/2026-07-09-b.json"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("list = %v, want %v (sorted, prefix-scoped)", got, want)
	}

	empty, err := f.List(ctx, "nothing/here/")
	if err != nil || len(empty) != 0 {
		t.Errorf("unused prefix: got %v, %v; want empty list, nil error", empty, err)
	}
}
