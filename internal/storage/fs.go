package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FS stores each key as a file under Root. It perserves the current
// AUTOMATION_ROOT-anchored layout, so migrating callers changes no paths
type FS struct {
	Root string
}

// NewFS anchors the store at AUTOMATION_ROOT (falling back to "."),
// matching the previous behavior of every call site.
func NewFS() *FS {
	root := os.Getenv("AUTOMATION_ROOT")
	if root == "" {
		root = "."
	}
	return &FS{Root: root}
}

func (f *FS) path(key string) string {
	return filepath.Join(f.Root, filepath.FromSlash(key))
}

func (f *FS) Get(_ context.Context, key string) ([]byte, error) {
	data, err := os.ReadFile(f.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("get %q: %w", key, ErrNotExist)
	}
	if err != nil {
		return nil, fmt.Errorf("get %q: %w", key, err)
	}
	return data, nil
}

func (f *FS) Put(_ context.Context, key string, data []byte) error {
	p := f.path(key)

	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("put %q: %w", key, err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return fmt.Errorf("put %q: %w", key, err)
	}
	return nil
}

func (f *FS) Append(_ context.Context, key string, line []byte) error {
	p := f.path(key)

	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("append %q: %w", key, err)
	}
	file, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("append %q: %w", key, err)
	}
	defer file.Close()

	if _, err := file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append %q: %w", key, err)
	}
	return nil
}
