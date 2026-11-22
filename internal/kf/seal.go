package kf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sl6117/automations/internal/corpus"
	"github.com/sl6117/automations/internal/storage"
)

// This file is the only part of kf that touches a store. Sealed revisions are
// append-only: an export writes a new revision or nothing, and never rewrites one.

const exportPrefix = "exports/v1/"

const (
	OutcomeSealed    = "sealed"
	OutcomeUnchanged = "unchanged"
)

// ErrNothingFetched means the corpus has no run record for the day: there is nothing
// to say about it, and an empty export would read as "zero mentions".
var ErrNothingFetched = errors.New("nothing fetched")

// RelPaths are the three files of one revision, relative to exports/v1/ and to --out.
func RelPaths(date string, rev int) (mentions, entityDay, manifest string) {
	suffix := fmt.Sprintf("%s.r%d", date, rev)
	return "mentions/" + suffix + ".jsonl", "entity_day/" + suffix + ".jsonl", "manifest/" + suffix + ".json"
}

type ExportResult struct {
	Date         string
	Outcome      string // OutcomeSealed or OutcomeUnchanged
	Revision     int    // the revision sealed, or the latest one when unchanged
	Supersedes   int    // 0 when this is r1 or unchanged
	Mentions     int
	EntityDays   int
	PostsFetched int
}

func loadDay(ctx context.Context, store storage.Store, day time.Time) (Day, error) {
	posts, err := corpus.Load(ctx, store, day, day)
	if err != nil {
		return Day{}, err
	}
	runs, err := corpus.LoadRuns(ctx, store, day, day)
	if err != nil {
		return Day{}, err
	}
	return Day{Date: day, Posts: posts, Runs: runs}, nil
}

// latestSealed probes manifest r1, r2, ... until one is missing. Never List: on Dynamo
// that is a full-table scan.
func latestSealed(ctx context.Context, store storage.Store, date string) (int, *Manifest, error) {
	var last *Manifest
	for rev := 1; ; rev++ {
		_, _, key := RelPaths(date, rev)
		data, err := store.Get(ctx, exportPrefix+key)
		if errors.Is(err, storage.ErrNotExist) {
			return rev - 1, last, nil
		}
		if err != nil {
			return 0, nil, fmt.Errorf("read manifest r%d: %w", rev, err)
		}
		var m Manifest
		if err := json.Unmarshal(data, &m); err != nil {
			return 0, nil, fmt.Errorf("parse manifest r%d: %w", rev, err)
		}
		last = &m
	}
}

func fileHash(m *Manifest, prefix string) string {
	for _, f := range m.Files {
		if strings.HasPrefix(f.Path, prefix) {
			return f.SHA256
		}
	}
	return ""
}

// Export seals day D. When the latest sealed revision already holds byte-identical data
// files it does nothing. Otherwise it writes r<N+1>: data files first, manifest last, so
// a crash leaves an unsealed revision that the next run overwrites.
func Export(ctx context.Context, store storage.Store, u *Universe, day time.Time, now func() time.Time) (ExportResult, error) {
	date := utcDate(day)
	dateStr := date.Format(dateLayout)
	res := ExportResult{Date: dateStr}
	if date.After(utcDate(now())) {
		return res, fmt.Errorf("refusing to export %s: it is in the future", dateStr)
	}

	today, err := loadDay(ctx, store, date)
	if err != nil {
		return res, err
	}
	if len(today.Runs) == 0 {
		return res, fmt.Errorf("%w on %s", ErrNothingFetched, dateStr)
	}
	history := make([]Day, 0, BaselineWindowDays)
	for i := 1; i <= BaselineWindowDays; i++ {
		d, err := loadDay(ctx, store, date.AddDate(0, 0, -i))
		if err != nil {
			return res, err
		}
		history = append(history, d)
	}

	built, err := BuildDay(u, today, history)
	if err != nil {
		return res, err
	}
	res.Mentions, res.EntityDays, res.PostsFetched = built.MentionCount, built.EntityDayCount, built.PostsFetched

	latest, sealed, err := latestSealed(ctx, store, dateStr)
	if err != nil {
		return res, err
	}
	mHash, eHash := sha256Hex(built.Mentions), sha256Hex(built.EntityDays)
	if sealed != nil && fileHash(sealed, "mentions/") == mHash && fileHash(sealed, "entity_day/") == eHash {
		res.Outcome, res.Revision = OutcomeUnchanged, latest
		return res, nil
	}

	rev := latest + 1
	mPath, ePath, manPath := RelPaths(dateStr, rev)
	manifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Date:          dateStr,
		Revision:      rev,
		SealedAt:      fetchStamp(now()),
		SourceWindow:  SourceWindow{FirstFetch: built.FirstFetch, LastFetch: built.LastFetch},
		Files: []ManifestFile{
			{Path: mPath, Records: built.MentionCount, SHA256: mHash},
			{Path: ePath, Records: built.EntityDayCount, SHA256: eHash},
		},
		UniverseVersion:  u.Version,
		ExtractorVersion: ExtractorVersion,
		PostsFetched:     built.PostsFetched,
		WindowTruncated:  built.WindowTruncated,
		WindowPartial:    built.WindowPartial,
		UnmappedCashtags: built.Unmapped,
	}
	if latest > 0 {
		prev := latest
		manifest.Supersedes = &prev
	}
	manBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return res, fmt.Errorf("marshal manifest: %w", err)
	}
	manBytes = append(manBytes, '\n')

	if err := store.Put(ctx, exportPrefix+mPath, built.Mentions); err != nil {
		return res, fmt.Errorf("write %s: %w", mPath, err)
	}
	if err := store.Put(ctx, exportPrefix+ePath, built.EntityDays); err != nil {
		return res, fmt.Errorf("write %s: %w", ePath, err)
	}
	if err := store.Put(ctx, exportPrefix+manPath, manBytes); err != nil {
		return res, fmt.Errorf("write %s: %w", manPath, err)
	}
	res.Outcome, res.Revision, res.Supersedes = OutcomeSealed, rev, latest
	return res, nil
}

// Materialize copies the latest sealed revision of a day into dir under the same
// relative paths, checking every data file against the manifest's sha256 on the way.
func Materialize(ctx context.Context, store storage.Store, day time.Time, dir string) (int, []string, error) {
	dateStr := utcDate(day).Format(dateLayout)
	rev, m, err := latestSealed(ctx, store, dateStr)
	if err != nil {
		return 0, nil, err
	}
	if m == nil {
		return 0, nil, fmt.Errorf("no sealed revision of %s", dateStr)
	}
	_, _, manPath := RelPaths(dateStr, rev)
	paths := []string{}
	for _, f := range m.Files {
		paths = append(paths, f.Path)
	}
	paths = append(paths, manPath)
	for _, rel := range paths {
		data, err := store.Get(ctx, exportPrefix+rel)
		if err != nil {
			return 0, nil, fmt.Errorf("read %s: %w", rel, err)
		}
		for _, f := range m.Files {
			if f.Path == rel && sha256Hex(data) != f.SHA256 {
				return 0, nil, fmt.Errorf("%s does not match its manifest sha256; refusing to materialize", rel)
			}
		}
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return 0, nil, err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return 0, nil, err
		}
	}
	return rev, paths, nil
}
