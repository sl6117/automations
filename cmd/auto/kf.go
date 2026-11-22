package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sl6117/automations/internal/corpus"
	"github.com/sl6117/automations/internal/kf"
	"github.com/sl6117/automations/internal/storage"
)

const defaultUniversePath = "projects/knowledge-factory/universe.json"

type kfExportOptions struct {
	Date         string
	Out          string
	AllowDynamo  bool
	UniversePath string
	Getenv       func(string) string
	OpenStore    func(context.Context) (storage.Store, error)
	Now          func() time.Time
}

func cmdKF(args []string) {
	if len(args) < 1 || args[0] != "export" {
		fmt.Fprintln(os.Stderr, "kf: usage: auto kf export --date YYYY-MM-DD [--out dir] [--allow-dynamo]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("kf export", flag.ExitOnError)
	date := fs.String("date", "", "UTC fetch day to seal, YYYY-MM-DD")
	out := fs.String("out", "", "also copy the latest sealed revision of the day into this directory")
	allowDynamo := fs.Bool("allow-dynamo", false, "permit sealing into the real corpus table when STORAGE_BACKEND=dynamo")
	universe := fs.String("universe", defaultUniversePath, "ticker universe file")
	fs.Parse(args[1:])

	err := kfExport(context.Background(), os.Stdout, kfExportOptions{
		Date: *date, Out: *out, AllowDynamo: *allowDynamo, UniversePath: *universe,
		Getenv: os.Getenv, OpenStore: corpus.StoreFromEnv, Now: time.Now,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "kf export: %v\n", err)
		os.Exit(1)
	}
}

// kfExport seals one day and optionally materializes it. It prints counts and the
// revision outcome only, never post text.
func kfExport(ctx context.Context, w io.Writer, o kfExportOptions) error {
	// Checked before any store exists: sealing writes permanent revisions, so reaching
	// the production table must be a deliberate choice, as with cmd/seed's explicit keys.
	if o.Getenv("STORAGE_BACKEND") == "dynamo" && !o.AllowDynamo {
		return errors.New("STORAGE_BACKEND=dynamo targets the real corpus table and sealed revisions are permanent; pass --allow-dynamo to proceed")
	}
	if o.Date == "" {
		return errors.New("--date YYYY-MM-DD is required")
	}
	day, err := time.Parse("2006-01-02", o.Date)
	if err != nil {
		return fmt.Errorf("--date %q: want YYYY-MM-DD", o.Date)
	}
	u, err := kf.LoadUniverse(o.UniversePath)
	if err != nil {
		return err
	}
	store, err := o.OpenStore(ctx)
	if err != nil {
		return fmt.Errorf("corpus store: %w", err)
	}

	res, err := kf.Export(ctx, store, u, day, o.Now)
	if err != nil {
		return err
	}
	switch {
	case res.Outcome == kf.OutcomeUnchanged:
		fmt.Fprintf(w, "%s: unchanged r%d", res.Date, res.Revision)
	case res.Supersedes > 0:
		fmt.Fprintf(w, "%s: sealed r%d supersedes r%d", res.Date, res.Revision, res.Supersedes)
	default:
		fmt.Fprintf(w, "%s: sealed r%d", res.Date, res.Revision)
	}
	fmt.Fprintf(w, " (mentions %d, entity_day %d, posts_fetched %d)\n", res.Mentions, res.EntityDays, res.PostsFetched)

	if o.Out != "" {
		rev, paths, err := kf.Materialize(ctx, store, day, o.Out)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "wrote r%d to %s: %d files\n", rev, o.Out, len(paths))
	}
	return nil
}
