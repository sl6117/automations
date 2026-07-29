package twitterdigest

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/sl6117/automations/internal/storage"
)

// citationID pulls the numeric status id out of a citation URL. The digest cites posts as
// "(@handle https://x.com/handle/status/<id>)" and the id is the only part of that the
// model cannot mangle: handles get case-folded and summaries get translated, ids do not.
var citationID = regexp.MustCompile(`/status/(\d+)`)

// CitationReport is one handle's editorial hit rate: of its posts that cleared the filter
// and reached the model, how many the published digest actually cited.
type CitationReport struct {
	Handle   string
	Kept     int
	Cited    int
	CiteRate float64
}

// CitationsSummary is the archive-wide view of what the model did with what it was fed.
// Its window is the digest archive, which starts months before the fetch archive, so
// these counts are NOT comparable to SourcesSummary.Kept.
type CitationsSummary struct {
	Digests            int
	SkippedDigests     int // published nothing, so exercised no editorial judgment
	Kept               int
	Cited              int
	CiteRate           float64
	UnmatchedCitations int // cited an id that was never fed to it: an invented URL
	FirstTimestamp     string
	LastTimestamp      string
	ByHandle           map[string]CitationReport
}

// parseCitations returns the status ids a digest cites, in order of first appearance and without repeats:
// the prompt asks for one citation per URL but the rate must not depend on the model obeying that.
func parseCitations(digest string) []string {
	var ids []string
	seen := map[string]bool{}
	for _, m := range citationID.FindAllStringSubmatch(digest, -1) {
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		ids = append(ids, m[1])
	}
	return ids
}

// LoadCitations reads every stored digest artifact, oldest first (keys sort lexically by timestamp)
// Since is an optional YYYY-MM-DD lower bound. Keys are matched on -"twitter-digest" without a trailing dash
// so the 4 artifacts written before the language suffix existed are included too.
func LoadCitations(ctx context.Context, store storage.Store, since string) ([]Artifact, error) {
	keys, err := store.List(ctx, artifactPrefix)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	artifacts := make([]Artifact, 0, len(keys))
	for _, key := range keys {
		name := strings.TrimPrefix(key, artifactPrefix)
		if !strings.Contains(name, "-twitter-digest") {
			continue
		}
		if since != "" && name < since {
			continue
		}
		data, err := store.Get(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("get %q: %w", key, err)
		}
		var a Artifact
		if err := json.Unmarshal(data, &a); err != nil {
			return nil, fmt.Errorf("parse %q: %w", key, err)
		}
		artifacts = append(artifacts, a)
	}
	return artifacts, nil
}

// SummarizeCitations aggregates digests into a per-handle hit rate. Counting is by distinct post id rather than by artifact row:
// one run is published once per language and each copy carries the same kept posts, so summing would double every count.
func SummarizeCitations(artifacts []Artifact) CitationsSummary {
	s := CitationsSummary{ByHandle: map[string]CitationReport{}}
	handleOf := map[string]string{} // post id -> handle
	cited := map[string]bool{}
	for _, a := range artifacts {
		// a run that failed before publishing exercised no judgment; counting its posts
		// as uncited would punish whichever handles happened to be in it
		if strings.TrimSpace(a.Digest) == "" {
			s.SkippedDigests++
			continue
		}
		s.Digests++
		if s.FirstTimestamp == "" || a.Timestamp < s.FirstTimestamp {
			s.FirstTimestamp = a.Timestamp
		}
		if a.Timestamp > s.LastTimestamp {
			s.LastTimestamp = a.Timestamp
		}
		for _, t := range a.Kept {
			if t.ID != "" {
				handleOf[t.ID] = t.Handle
			}
		}
		for _, id := range parseCitations(a.Digest) {
			cited[id] = true
		}
	}
	for id, handle := range handleOf {
		r := s.ByHandle[handle]
		r.Handle = handle
		r.Kept++
		if cited[id] {
			r.Cited++
		}
		s.ByHandle[handle] = r
	}
	for id := range cited {
		if _, ok := handleOf[id]; !ok {
			s.UnmatchedCitations++
		}
	}
	for handle, r := range s.ByHandle {
		if r.Kept > 0 {
			r.CiteRate = float64(r.Cited) / float64(r.Kept)
		}
		s.ByHandle[handle] = r
		s.Kept += r.Kept
		s.Cited += r.Cited
	}
	if s.Kept > 0 {
		s.CiteRate = float64(s.Cited) / float64(s.Kept)
	}
	return s
}
