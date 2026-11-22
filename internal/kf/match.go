package kf

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sl6117/automations/internal/corpus"
)

// ExtractorVersion names the matching rules below. Bump it whenever a rule changes, so a
// consumer can tell a rule change from a change in what the accounts said.
const ExtractorVersion = "kf.extract.v1"

const (
	KindCashtag = "cashtag"
	KindAlias   = "alias"
	InOwn       = "own"
	InRef       = "ref"
)

// Mention is one (post, ticker, kind, in) hit. Surface is the first match as written.
type Mention struct {
	PostID  string `json:"post_id"`
	Ticker  string `json:"ticker"`
	Kind    string `json:"kind"`
	In      string `json:"in"`
	Surface string `json:"surface"`
}

// UnmappedCashtag counts distinct posts carrying a well-formed cashtag outside the
// universe, so the owner can see what to add. Symbols only.
type UnmappedCashtag struct {
	Symbol string `json:"symbol"`
	Posts  int    `json:"posts"`
}

// hit is a mention plus which ref it came from (-1 for own text), which the voice count needs.
type hit struct {
	Mention
	ref int
}

// cashtag is one well-formed $SYMBOL in a text, before the universe check.
type cashtag struct {
	symbol  string // upper case; includes a .X class suffix when one was written
	base    string // symbol without the class suffix
	surface string
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func isASCIILetter(b byte) bool { return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') }

// runeBefore and runeAfter return utf8.RuneError at the ends of s, which is not a word rune.
func runeBefore(s string, i int) rune {
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return r
}

func runeAfter(s string, i int) rune {
	r, _ := utf8.DecodeRuneInString(s[i:])
	return r
}

// findCashtags returns every "$" + 1-5 ASCII letters (+ optional ".X") that is not glued
// to a letter or digit on either side. "$100", "US$5" and "$EXMPLX1" are not cashtags.
func findCashtags(s string) []cashtag {
	var out []cashtag
	for i := 0; i < len(s); i++ {
		if s[i] != '$' || isWordRune(runeBefore(s, i)) {
			continue
		}
		j := i + 1
		for j < len(s) && isASCIILetter(s[j]) {
			j++
		}
		n := j - (i + 1)
		if n < 1 || n > 5 || isWordRune(runeAfter(s, j)) {
			continue
		}
		base := strings.ToUpper(s[i+1 : j])
		c := cashtag{symbol: base, base: base, surface: s[i:j]}
		// a class suffix is ".X" closed by a non-word rune; "$EXMPL.Next" stays $EXMPL
		if j+1 < len(s) && s[j] == '.' && isASCIILetter(s[j+1]) && !isWordRune(runeAfter(s, j+2)) {
			c.symbol = base + "." + strings.ToUpper(s[j+1:j+2])
			c.surface = s[i : j+2]
		}
		out = append(out, c)
		i = j - 1
	}
	return out
}

// resolve maps a cashtag to a universe ticker. A written class suffix wins when that
// class is listed. When only the base is listed, the suffix is read as punctuation
// ("$EXMPL.A great quarter"), so the base still counts.
func (u *Universe) resolve(c cashtag) (string, string, bool) {
	if u.Has(c.symbol) {
		return c.symbol, c.surface, true
	}
	if c.symbol != c.base && u.Has(c.base) {
		return c.base, c.surface[:len(c.base)+1], true
	}
	return "", "", false
}

// findPhrase returns the byte offsets of every whole-phrase, case-sensitive occurrence of
// phrase in s. A letter or digit on either side blocks the match, as does a leading "@":
// "@Example" is an account handle, not the company named in running text.
func findPhrase(s, phrase string) []int {
	var out []int
	for from := 0; from <= len(s)-len(phrase); {
		k := strings.Index(s[from:], phrase)
		if k < 0 {
			break
		}
		at := from + k
		before := runeBefore(s, at)
		if !isWordRune(before) && before != '@' && !isWordRune(runeAfter(s, at+len(phrase))) {
			out = append(out, at)
		}
		_, size := utf8.DecodeRuneInString(s[at:])
		from = at + size
	}
	return out
}

// scanText returns the first hit per (ticker, kind) in one text, in text order.
func (u *Universe) scanText(s, in string, ref int, postID string) []hit {
	var hits []hit
	seen := map[[2]string]bool{}
	add := func(ticker, kind, surface string) {
		key := [2]string{ticker, kind}
		if seen[key] {
			return
		}
		seen[key] = true
		hits = append(hits, hit{Mention{PostID: postID, Ticker: ticker, Kind: kind, In: in, Surface: surface}, ref})
	}
	for _, c := range findCashtags(s) {
		if ticker, surface, ok := u.resolve(c); ok {
			add(ticker, KindCashtag, surface)
		}
	}
	for _, a := range u.aliases {
		if len(findPhrase(s, a.alias)) > 0 {
			add(a.ticker, KindAlias, a.alias)
		}
	}
	return hits
}

// scanPost scans the author's own text and each referenced post separately. Text is
// never scanned: it is the flattened concatenation and would count a quote twice.
func (u *Universe) scanPost(p corpus.StoredPost) []hit {
	hits := u.scanText(p.OwnText, InOwn, -1, p.ID)
	for i, r := range p.Refs {
		hits = append(hits, u.scanText(r.Text, InRef, i, p.ID)...)
	}
	return hits
}

// livePosts drops backfilled posts and keeps one copy per id (the latest fetch), sorted
// by id, so every output is independent of input order and duplicates.
func livePosts(posts []corpus.StoredPost) []corpus.StoredPost {
	latest := map[string]corpus.StoredPost{}
	for _, p := range posts {
		if p.Backfilled {
			continue
		}
		if prev, ok := latest[p.ID]; !ok || p.FetchedAt.After(prev.FetchedAt) {
			latest[p.ID] = p
		}
	}
	out := make([]corpus.StoredPost, 0, len(latest))
	for _, p := range latest {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return idLess(out[i].ID, out[j].ID) })
	return out
}

// Mentions returns at most one mention per (post, ticker, kind, in), sorted by post id
// (numeric), ticker, kind, in. Several matching refs give one "ref" mention.
func Mentions(u *Universe, posts []corpus.StoredPost) []Mention {
	var out []Mention
	for _, p := range livePosts(posts) {
		seen := map[Mention]bool{}
		for _, h := range u.scanPost(p) {
			key := Mention{PostID: h.PostID, Ticker: h.Ticker, Kind: h.Kind, In: h.In}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, h.Mention)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.PostID != b.PostID {
			return idLess(a.PostID, b.PostID)
		}
		if a.Ticker != b.Ticker {
			return a.Ticker < b.Ticker
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.In < b.In
	})
	return out
}

// UnmappedCashtags tallies well-formed cashtags the universe does not resolve, by
// distinct posts, most frequent first then by symbol.
func UnmappedCashtags(u *Universe, posts []corpus.StoredPost) []UnmappedCashtag {
	counts := map[string]int{}
	for _, p := range livePosts(posts) {
		seen := map[string]bool{}
		texts := []string{p.OwnText}
		for _, r := range p.Refs {
			texts = append(texts, r.Text)
		}
		for _, t := range texts {
			for _, c := range findCashtags(t) {
				if _, _, ok := u.resolve(c); ok || seen[c.symbol] {
					continue
				}
				seen[c.symbol] = true
				counts[c.symbol]++
			}
		}
	}
	out := make([]UnmappedCashtag, 0, len(counts))
	for s, n := range counts {
		out = append(out, UnmappedCashtag{Symbol: s, Posts: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Posts != out[j].Posts {
			return out[i].Posts > out[j].Posts
		}
		return out[i].Symbol < out[j].Symbol
	})
	return out
}

// idLess orders X ids numerically; a non-numeric id sorts after numeric ones, then lexically.
func idLess(a, b string) bool {
	na, errA := strconv.ParseUint(a, 10, 64)
	nb, errB := strconv.ParseUint(b, 10, 64)
	switch {
	case errA == nil && errB == nil:
		return na < nb
	case errA == nil:
		return true
	case errB == nil:
		return false
	default:
		return a < b
	}
}
