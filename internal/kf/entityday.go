package kf

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/sl6117/automations/internal/corpus"
)

const (
	EntityDaySchemaVersion = "kf.entity_day.v1"

	// BaselineWindowDays and MinBaselineDays are fixed by the contract. Changing either
	// changes what z_share means, which is a v2 feed, not a tuning step.
	BaselineWindowDays = 14
	MinBaselineDays    = 10

	dateLayout = "2006-01-02"
)

// Day is one UTC fetch day as loaded from the corpus: corpus.Load and corpus.LoadRuns
// over that single day. Loading each day on its own keeps a later re-fetch of a post
// from moving it out of an earlier day's counts.
type Day struct {
	Date  time.Time
	Posts []corpus.StoredPost
	Runs  []corpus.RunRecord
}

type Baseline struct {
	Days      int     `json:"days"`
	MeanShare float64 `json:"mean_share"`
	StdShare  float64 `json:"std_share"`
}

// EntityDay is one ticker on one day, field for field the kf.entity_day.v1 record.
type EntityDay struct {
	SchemaVersion   string    `json:"schema_version"`
	Ticker          string    `json:"ticker"`
	Date            string    `json:"date"`
	Posts           int       `json:"posts"`
	PostsCashtag    int       `json:"posts_cashtag"`
	PostsOwn        int       `json:"posts_own"`
	UniqueVoices    int       `json:"unique_voices"`
	PostsFetched    int       `json:"posts_fetched"`
	Share           float64   `json:"share"`
	Baseline        Baseline  `json:"baseline"`
	ZShare          *float64  `json:"z_share"` // null when the baseline is too short or flat
	WindowTruncated bool      `json:"window_truncated"`
	WindowPartial   bool      `json:"window_partial"`
	AvailableAt     time.Time `json:"available_at"`
}

// dayStats is what one day contributes: its denominator and per-ticker tallies.
type dayStats struct {
	fetched                 int
	posts, cashtag, own     map[string]map[string]bool // ticker -> post ids
	voices                  map[string]map[string]bool // ticker -> voice keys
	truncated, partial, any bool
	availableAt             time.Time
	firstFetch              time.Time // earliest run that day; the manifest's source window
}

func utcDate(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// voice is who said it: the author for own words, the referenced post's author for a
// ref, so three retweets of one post are one voice. Falls back to the author id, then to
// the referenced post id, so an unknown author is still one voice per original post.
func voice(p corpus.StoredPost, h hit) string {
	handle, id, fallback := p.Handle, p.AuthorID, "post:"+p.ID
	if h.ref >= 0 {
		r := p.Refs[h.ref]
		handle, id, fallback = r.AuthorHandle, r.AuthorID, "post:"+r.ID
	}
	if h := strings.ToLower(strings.TrimPrefix(handle, "@")); h != "" {
		return "handle:" + h
	}
	if id != "" {
		return "id:" + id
	}
	return fallback
}

func add(m map[string]map[string]bool, ticker, key string) {
	if m[ticker] == nil {
		m[ticker] = map[string]bool{}
	}
	m[ticker][key] = true
}

// stats tallies one day. Posts and runs fetched on another UTC date are ignored, so a
// caller's off-by-one cannot leak a neighbour's data into the day.
func stats(u *Universe, d Day) dayStats {
	date := utcDate(d.Date)
	s := dayStats{
		posts: map[string]map[string]bool{}, cashtag: map[string]map[string]bool{},
		own: map[string]map[string]bool{}, voices: map[string]map[string]bool{},
	}
	for _, r := range d.Runs {
		if !utcDate(r.FetchedAt).Equal(date) {
			continue
		}
		s.any = true
		s.truncated = s.truncated || r.Truncated
		s.partial = s.partial || r.Written < r.Fetched
		if r.FetchedAt.After(s.availableAt) {
			s.availableAt = r.FetchedAt.UTC()
		}
		if s.firstFetch.IsZero() || r.FetchedAt.Before(s.firstFetch) {
			s.firstFetch = r.FetchedAt.UTC()
		}
	}
	var sameDay []corpus.StoredPost
	for _, p := range d.Posts {
		if utcDate(p.FetchedAt).Equal(date) {
			sameDay = append(sameDay, p)
		}
	}
	for _, p := range livePosts(sameDay) {
		s.fetched++
		if !s.any && p.FetchedAt.After(s.availableAt) {
			s.availableAt = p.FetchedAt.UTC() // no run record: the posts are all we know
		}
		for _, h := range u.scanPost(p) {
			add(s.posts, h.Ticker, p.ID)
			add(s.voices, h.Ticker, voice(p, h))
			if h.Kind == KindCashtag {
				add(s.cashtag, h.Ticker, p.ID)
			}
			if h.In == InOwn {
				add(s.own, h.Ticker, p.ID)
			}
		}
	}
	return s
}

func (s dayStats) share(ticker string) float64 {
	if s.fetched == 0 {
		return 0
	}
	return float64(len(s.posts[ticker])) / float64(s.fetched)
}

// EntityDays returns one row per ticker mentioned on today.Date, sorted by ticker.
// history may hold any days; only the BaselineWindowDays before today that have at least
// one run record form the baseline (a day with data and no mention of a ticker is share
// 0; a day with no run record is absent, not zero). Backfilled posts count nowhere.
func EntityDays(u *Universe, today Day, history []Day) []EntityDay {
	date := utcDate(today.Date)
	cur := stats(u, today)

	byDate := map[time.Time]Day{}
	for _, d := range history {
		k := utcDate(d.Date)
		if k.Before(date.AddDate(0, 0, -BaselineWindowDays)) || !k.Before(date) {
			continue
		}
		merged := byDate[k]
		merged.Date = k
		merged.Posts = append(merged.Posts, d.Posts...)
		merged.Runs = append(merged.Runs, d.Runs...)
		byDate[k] = merged
	}
	var window []dayStats
	dates := make([]time.Time, 0, len(byDate))
	for k := range byDate {
		dates = append(dates, k)
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
	for _, k := range dates {
		if s := stats(u, byDate[k]); s.any {
			window = append(window, s)
		}
	}

	tickers := make([]string, 0, len(cur.posts))
	for t := range cur.posts {
		tickers = append(tickers, t)
	}
	sort.Strings(tickers)

	rows := make([]EntityDay, 0, len(tickers))
	for _, t := range tickers {
		row := EntityDay{
			SchemaVersion:   EntityDaySchemaVersion,
			Ticker:          t,
			Date:            date.Format(dateLayout),
			Posts:           len(cur.posts[t]),
			PostsCashtag:    len(cur.cashtag[t]),
			PostsOwn:        len(cur.own[t]),
			UniqueVoices:    len(cur.voices[t]),
			PostsFetched:    cur.fetched,
			Share:           cur.share(t),
			WindowTruncated: cur.truncated,
			WindowPartial:   cur.partial,
			AvailableAt:     cur.availableAt,
		}
		row.Baseline = baseline(window, t)
		if row.Baseline.Days >= MinBaselineDays && row.Baseline.StdShare > 0 {
			z := (row.Share - row.Baseline.MeanShare) / row.Baseline.StdShare
			row.ZShare = &z
		}
		rows = append(rows, row)
	}
	return rows
}

// baseline is the mean and sample (n-1) standard deviation of the ticker's share over the
// window days, summed in date order so the floats are identical on every run.
func baseline(window []dayStats, ticker string) Baseline {
	b := Baseline{Days: len(window)}
	if b.Days == 0 {
		return b
	}
	sum := 0.0
	for _, s := range window {
		sum += s.share(ticker)
	}
	b.MeanShare = sum / float64(b.Days)
	flat := true
	for _, s := range window {
		flat = flat && s.share(ticker) == window[0].share(ticker)
	}
	// a flat series must give exactly 0, not rounding noise that makes z enormous
	if flat {
		b.MeanShare = window[0].share(ticker)
		return b
	}
	if b.Days > 1 {
		ss := 0.0
		for _, s := range window {
			d := s.share(ticker) - b.MeanShare
			ss += d * d
		}
		b.StdShare = math.Sqrt(ss / float64(b.Days-1))
	}
	return b
}
