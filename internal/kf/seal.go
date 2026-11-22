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

// compute loads day D and its baseline window from the corpus and builds the data files.
func compute(ctx context.Context, store storage.Store, u *Universe, day time.Time, now func() time.Time) (DayExport, ExportResult, error) {
	date := utcDate(day)
	dateStr := date.Format(dateLayout)
	res := ExportResult{Date: dateStr}
	if date.After(utcDate(now())) {
		return DayExport{}, res, fmt.Errorf("refusing to export %s: it is in the future", dateStr)
	}
	today, err := loadDay(ctx, store, date)
	if err != nil {
		return DayExport{}, res, err
	}
	if len(today.Runs) == 0 {
		return DayExport{}, res, fmt.Errorf("%w on %s", ErrNothingFetched, dateStr)
	}
	history := make([]Day, 0, BaselineWindowDays)
	for i := 1; i <= BaselineWindowDays; i++ {
		d, err := loadDay(ctx, store, date.AddDate(0, 0, -i))
		if err != nil {
			return DayExport{}, res, err
		}
		history = append(history, d)
	}
	built, err := BuildDay(u, today, history)
	if err != nil {
		return DayExport{}, res, err
	}
	res.Mentions, res.EntityDays, res.PostsFetched = built.MentionCount, built.EntityDayCount, built.PostsFetched
	return built, res, nil
}

// Preview computes what Export would seal for day D, with the same errors, and writes
// nothing. Outcome and Revision are left empty. It backs dry runs.
func Preview(ctx context.Context, store storage.Store, u *Universe, day time.Time, now func() time.Time) (ExportResult, error) {
	_, res, err := compute(ctx, store, u, day, now)
	return res, err
}

// Export seals day D. When the latest sealed revision already holds byte-identical data
// files it does nothing. Otherwise it writes r<N+1>: data files first, manifest last, so
// a crash leaves an unsealed revision that the next run overwrites.
func Export(ctx context.Context, store storage.Store, u *Universe, day time.Time, now func() time.Time) (ExportResult, error) {
	built, res, err := compute(ctx, store, u, day, now)
	if err != nil {
		return res, err
	}
	dateStr := res.Date

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

	// An empty data file is not stored at all: DynamoDB has rejected empty values here
	// before. The manifest (records 0, sha256 of nothing) is what says the file is empty.
	for _, f := range []struct {
		path string
		data []byte
	}{{mPath, built.Mentions}, {ePath, built.EntityDays}} {
		if len(f.data) == 0 {
			continue
		}
		if err := store.Put(ctx, exportPrefix+f.path, f.data); err != nil {
			return res, fmt.Errorf("write %s: %w", f.path, err)
		}
	}
	if err := store.Put(ctx, exportPrefix+manPath, manBytes); err != nil {
		return res, fmt.Errorf("write %s: %w", manPath, err)
	}
	res.Outcome, res.Revision, res.Supersedes = OutcomeSealed, rev, latest
	return res, nil
}

// emptySHA256 is the sha256 of zero bytes: a manifest entry with this hash and zero
// records describes a data file that was never stored.
var emptySHA256 = sha256Hex(nil)

// Sealed returns the latest sealed revision of day, or 0 when none exists. It writes nothing.
func Sealed(ctx context.Context, store storage.Store, day time.Time) (int, error) {
	rev, _, err := latestSealed(ctx, store, utcDate(day).Format(dateLayout))
	return rev, err
}

// File is one published file of a revision, by its path relative to exports/v1/.
type File struct {
	Path string
	Data []byte
}

// RevisionFiles returns a sealed revision's files in publishing order: the data files
// first and the manifest last, so a copy that has the manifest has everything. Each data
// file is checked against the manifest. A file the manifest declares empty is returned
// as zero bytes without being read, so a leftover from a crashed attempt is never used.
func RevisionFiles(ctx context.Context, store storage.Store, day time.Time, rev int) ([]File, error) {
	date := utcDate(day).Format(dateLayout)
	_, _, manPath := RelPaths(date, rev)
	manBytes, err := store.Get(ctx, exportPrefix+manPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", manPath, err)
	}
	var m Manifest
	if err := json.Unmarshal(manBytes, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", manPath, err)
	}
	var files []File
	for _, f := range m.Files {
		if f.Records == 0 && f.SHA256 == emptySHA256 {
			files = append(files, File{Path: f.Path, Data: []byte{}})
			continue
		}
		data, err := store.Get(ctx, exportPrefix+f.Path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f.Path, err)
		}
		if sha256Hex(data) != f.SHA256 {
			return nil, fmt.Errorf("%s does not match its manifest sha256", f.Path)
		}
		files = append(files, File{Path: f.Path, Data: data})
	}
	return append(files, File{Path: manPath, Data: manBytes}), nil
}

// Materialize copies the latest sealed revision of a day into dir under the same
// relative paths, checking every data file against the manifest's sha256 on the way.
func Materialize(ctx context.Context, store storage.Store, day time.Time, dir string) (int, []string, error) {
	rev, err := Sealed(ctx, store, day)
	if err != nil {
		return 0, nil, err
	}
	if rev == 0 {
		return 0, nil, fmt.Errorf("no sealed revision of %s", utcDate(day).Format(dateLayout))
	}
	files, err := RevisionFiles(ctx, store, day, rev)
	if err != nil {
		return 0, nil, fmt.Errorf("refusing to materialize: %w", err)
	}
	var paths []string
	for _, f := range files {
		dst := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return 0, nil, err
		}
		if err := os.WriteFile(dst, f.Data, 0o644); err != nil {
			return 0, nil, err
		}
		paths = append(paths, f.Path)
	}
	return rev, paths, nil
}
