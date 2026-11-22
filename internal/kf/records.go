package kf

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/sl6117/automations/internal/corpus"
)

const (
	MentionSchemaVersion  = "kf.mention.v1"
	ManifestSchemaVersion = "kf.manifest.v1"
	SourcePack            = "core"

	maxUnmappedInManifest = 20
)

// The record structs below are the export allowlist: each has exactly the contract's
// fields and is filled field by field. Nothing reaches a file by dumping a StoredPost
// or a Mention, so post text cannot leak in through a field added upstream.

type MentionPost struct {
	ID              string `json:"id"`
	URL             string `json:"url"`
	AuthorID        string `json:"author_id,omitempty"`
	AuthorHandle    string `json:"author_handle"`
	CreatedAt       string `json:"created_at,omitempty"`
	RefType         string `json:"ref_type,omitempty"`
	RefPostID       string `json:"ref_post_id,omitempty"`
	RefAuthorHandle string `json:"ref_author_handle,omitempty"`
}

type MentionMetrics struct {
	Likes           int `json:"likes"`
	Reposts         int `json:"reposts"`
	Replies         int `json:"replies"`
	Quotes          int `json:"quotes"`
	Impressions     int `json:"impressions"`
	AuthorFollowers int `json:"author_followers"`
}

type MentionRecord struct {
	SchemaVersion string         `json:"schema_version"`
	RecordID      string         `json:"record_id"`
	Ticker        string         `json:"ticker"`
	Kind          string         `json:"kind"`
	In            string         `json:"in"`
	Post          MentionPost    `json:"post"`
	AvailableAt   string         `json:"available_at"`
	AgeAtFetchS   *int64         `json:"age_at_fetch_s,omitempty"`
	Metrics       MentionMetrics `json:"metrics"`
	SourcePack    string         `json:"source_pack"`
}

type EntityDayRecord struct {
	SchemaVersion   string   `json:"schema_version"`
	Ticker          string   `json:"ticker"`
	Date            string   `json:"date"`
	Posts           int      `json:"posts"`
	PostsCashtag    int      `json:"posts_cashtag"`
	PostsOwn        int      `json:"posts_own"`
	UniqueVoices    int      `json:"unique_voices"`
	PostsFetched    int      `json:"posts_fetched"`
	Share           float64  `json:"share"`
	Baseline        Baseline `json:"baseline"`
	ZShare          *float64 `json:"z_share"`
	WindowTruncated bool     `json:"window_truncated"`
	WindowPartial   bool     `json:"window_partial"`
	AvailableAt     string   `json:"available_at"`
}

type SourceWindow struct {
	FirstFetch string `json:"first_fetch"`
	LastFetch  string `json:"last_fetch"`
}

type ManifestFile struct {
	Path    string `json:"path"`
	Records int    `json:"records"`
	SHA256  string `json:"sha256"`
}

type Manifest struct {
	SchemaVersion    string            `json:"schema_version"`
	Date             string            `json:"date"`
	Revision         int               `json:"revision"`
	SealedAt         string            `json:"sealed_at"`
	SourceWindow     SourceWindow      `json:"source_window"`
	Files            []ManifestFile    `json:"files"`
	UniverseVersion  string            `json:"universe_version"`
	ExtractorVersion string            `json:"extractor_version"`
	PostsFetched     int               `json:"posts_fetched"`
	WindowTruncated  bool              `json:"window_truncated"`
	WindowPartial    bool              `json:"window_partial"`
	UnmappedCashtags []UnmappedCashtag `json:"unmapped_cashtags"`
	Supersedes       *int              `json:"supersedes"`
}

// fetchStamp rounds a fetch time UP to the second. available_at is the earliest moment
// the desk may use a record; rounding down would let it act before we had the post.
func fetchStamp(t time.Time) string {
	t = t.UTC()
	if s := t.Truncate(time.Second); !s.Equal(t) {
		t = s.Add(time.Second)
	}
	return t.Format(time.RFC3339)
}

// postStamp truncates a post's own timestamp; X sends whole seconds anyway.
func postStamp(t time.Time) string { return t.UTC().Truncate(time.Second).Format(time.RFC3339) }

// RecordID is the stable identity of a mention across revisions.
func RecordID(postID, ticker, kind, in string) string {
	sum := sha256.Sum256([]byte(MentionSchemaVersion + "|" + postID + "|" + ticker + "|" + kind + "|" + in))
	return hex.EncodeToString(sum[:])
}

func mentionRecord(m Mention, p corpus.StoredPost) MentionRecord {
	r := MentionRecord{
		SchemaVersion: MentionSchemaVersion,
		RecordID:      RecordID(m.PostID, m.Ticker, m.Kind, m.In),
		Ticker:        m.Ticker,
		Kind:          m.Kind,
		In:            m.In,
		Post: MentionPost{
			ID: p.ID, URL: p.URL, AuthorID: p.AuthorID, AuthorHandle: p.Handle,
		},
		AvailableAt: fetchStamp(p.FetchedAt),
		Metrics: MentionMetrics{
			Likes: p.Likes, Reposts: p.Reposts, Replies: p.Replies, Quotes: p.Quotes,
			Impressions: p.Impressions, AuthorFollowers: p.AuthorFollowers,
		},
		SourcePack: SourcePack,
	}
	if !p.CreatedAt.IsZero() {
		r.Post.CreatedAt = postStamp(p.CreatedAt)
		avail, _ := time.Parse(time.RFC3339, r.AvailableAt)
		created, _ := time.Parse(time.RFC3339, r.Post.CreatedAt)
		age := int64(avail.Sub(created) / time.Second)
		r.AgeAtFetchS = &age
	}
	if len(p.Refs) > 0 {
		ref := p.Refs[0]
		r.Post.RefType, r.Post.RefPostID, r.Post.RefAuthorHandle = ref.Type, ref.ID, ref.AuthorHandle
	}
	return r
}

func entityDayRecord(e EntityDay) EntityDayRecord {
	return EntityDayRecord{
		SchemaVersion: e.SchemaVersion, Ticker: e.Ticker, Date: e.Date,
		Posts: e.Posts, PostsCashtag: e.PostsCashtag, PostsOwn: e.PostsOwn, UniqueVoices: e.UniqueVoices,
		PostsFetched: e.PostsFetched, Share: e.Share, Baseline: e.Baseline, ZShare: e.ZShare,
		WindowTruncated: e.WindowTruncated, WindowPartial: e.WindowPartial, AvailableAt: fetchStamp(e.AvailableAt),
	}
}

// DayExport is one day's computed data files, before any revision number is attached.
// It holds no wall-clock value, so recomputing an unchanged day gives identical bytes.
type DayExport struct {
	Date            string
	Mentions        []byte
	MentionCount    int
	EntityDays      []byte
	EntityDayCount  int
	PostsFetched    int
	WindowTruncated bool
	WindowPartial   bool
	FirstFetch      string
	LastFetch       string
	Unmapped        []UnmappedCashtag // top maxUnmappedInManifest
}

func jsonLines[T any](records []T) ([]byte, error) {
	var buf bytes.Buffer
	for _, r := range records {
		line, err := json.Marshal(r)
		if err != nil {
			return nil, err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// BuildDay computes the two data files for today from the corpus. Posts fetched on
// another date are ignored, as in EntityDays.
func BuildDay(u *Universe, today Day, history []Day) (DayExport, error) {
	date := utcDate(today.Date)
	var sameDay []corpus.StoredPost
	for _, p := range today.Posts {
		if utcDate(p.FetchedAt).Equal(date) {
			sameDay = append(sameDay, p)
		}
	}
	live := livePosts(sameDay)
	byID := make(map[string]corpus.StoredPost, len(live))
	for _, p := range live {
		byID[p.ID] = p
	}

	mentions := Mentions(u, live)
	recs := make([]MentionRecord, len(mentions))
	for i, m := range mentions {
		recs[i] = mentionRecord(m, byID[m.PostID])
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].RecordID < recs[j].RecordID })

	rows := EntityDays(u, today, history)
	erecs := make([]EntityDayRecord, len(rows))
	for i, e := range rows {
		erecs[i] = entityDayRecord(e)
	}

	mb, err := jsonLines(recs)
	if err != nil {
		return DayExport{}, fmt.Errorf("marshal mentions: %w", err)
	}
	eb, err := jsonLines(erecs)
	if err != nil {
		return DayExport{}, fmt.Errorf("marshal entity_day: %w", err)
	}

	s := stats(u, today)
	unmapped := UnmappedCashtags(u, live)
	if len(unmapped) > maxUnmappedInManifest {
		unmapped = unmapped[:maxUnmappedInManifest]
	}
	d := DayExport{
		Date: date.Format(dateLayout), Mentions: mb, MentionCount: len(recs), EntityDays: eb, EntityDayCount: len(erecs),
		PostsFetched: s.fetched, WindowTruncated: s.truncated, WindowPartial: s.partial, Unmapped: unmapped,
	}
	if s.any {
		d.FirstFetch, d.LastFetch = fetchStamp(s.firstFetch), fetchStamp(s.availableAt)
	}
	return d, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
