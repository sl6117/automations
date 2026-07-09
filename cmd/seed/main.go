// One-off: copy current filesystem state into the DynamoDB backend
// before flipping STORAGE_BACKEND. Safe to re-run (Puts overwrite).
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/sl6117/automations/internal/storage"
)

func main() {
	ctx := context.Background()
	fs := storage.NewFS()
	ddb, err := storage.NewDynamo(ctx)
	if err != nil {
		log.Fatalf("dynamo: %v", err)
	}
	// Whole blobs: cursor state and subscribers.
	for _, key := range []string{
		"projects/twitter-digest/state.json",
		"projects/twitter-digest/subscribers.json",
	} {
		data, err := fs.Get(ctx, key)
		if errors.Is(err, storage.ErrNotExist) {
			log.Printf("skip %s (not on filesystem)", key)
			continue
		}
		if err != nil {
			log.Fatalf("read %s: %v", key, err)
		}
		if err := ddb.Put(ctx, key, data); err != nil {
			log.Fatalf("put %s: %v", key, err)
		}
		fmt.Printf("seeded %s (%d bytes)\n", key, len(data))
	}
	// Cost log: replay line by line so it lands as an append-stream.
	const costKey = "logs/cost-log.jsonl"
	data, err := fs.Get(ctx, costKey)
	if err != nil {
		log.Fatalf("read %s: %v", costKey, err)
	}
	n := 0
	scanner := bufio.NewScanner(bytes.NewReader(data))

	if err := ddb.DeleteAll(ctx, costKey); err != nil {
		log.Fatalf("wipe %s: %v", costKey, err)
	}
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := ddb.Append(ctx, costKey, line); err != nil {
			log.Fatalf("append cost line: %v", err)
		}
		n++
	}
	fmt.Printf("seeded %s (%d lines)\n", costKey, n)
}
