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
