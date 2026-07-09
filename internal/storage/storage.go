// Package storage abstracts where persistent data lives (filesystem now, DDB later)
// behind a minimal blob-store interface
package storage

import (
	"context"
	"errors"
	"os"
)

// ErrNotExist is returned by Get when the key has never been written
// implementations transalte their native "missing" error into this one
// so callers never depend on os.ErrNotExist or an AWS SDK error type

var ErrNotExist = errors.New("storage: key not found")

// Store is the contract every backend satisfies. Keys are slash-separated
// paths like "projects/twitter-digest/state.json"
type Store interface {
	// Get retrieves the value for a key. If the key is not found, ErrNotExist is returned
	Get(ctx context.Context, key string) ([]byte, error)

	// Put writes the value for a key. If the key already exists, it is overwritten
	Put(ctx context.Context, key string, data []byte) error

	// Append adds one line to the stream at key (for jsonl-style logs)
	Append(ctx context.Context, key string, line []byte) error
}

func FromEnv(ctx context.Context) (Store, error) {
	if os.Getenv("STORAGE_BACKEND") == "dynamo" {
		return NewDynamo(ctx)
	}
	return NewFS(), nil
}
