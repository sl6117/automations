// Package kf derives the desk-feed records (ticker mentions and the daily aggregate) from
// stored corpus posts and seals them as daily export files. Everything except seal.go
// is a pure function of its inputs: no store, no network, no model, no wall clock, so a
// sealed day can be recomputed byte for byte. The contract is docs/contracts/desk-feed-v1.md.
package kf

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	TypeEquity = "equity"
	TypeETF    = "etf"

	minAliasRunes = 4
)

var tickerPattern = regexp.MustCompile(`^[A-Z]{1,5}(\.[A-Z])?$`)

// Entry is one allowlisted instrument. Aliases are company names matched case-sensitively
// as whole phrases; they are the precision risk, so most entries have none.
type Entry struct {
	Ticker  string   `json:"ticker"`
	Type    string   `json:"type"`
	Aliases []string `json:"aliases"`
}

// Universe is the allowlist that enforces "US equities and ETFs only": a symbol that is
// not in it is never emitted. Build it with ParseUniverse or LoadUniverse, which validate.
type Universe struct {
	Version string  `json:"version"`
	Entries []Entry `json:"entries"`

	byTicker map[string]Entry
	aliases  []aliasEntry // longest first, so a longer phrase is tried before a shorter one
}

type aliasEntry struct{ alias, ticker string }

// LoadUniverse reads and validates a universe file.
func LoadUniverse(path string) (*Universe, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read universe: %w", err)
	}
	return ParseUniverse(data)
}

// ParseUniverse decodes and validates a universe.
func ParseUniverse(data []byte) (*Universe, error) {
	var u Universe
	if err := json.Unmarshal(data, &u); err != nil {
		return nil, fmt.Errorf("parse universe: %w", err)
	}
	return NewUniverse(u.Version, u.Entries)
}

// NewUniverse validates entries and builds the lookup indexes.
func NewUniverse(version string, entries []Entry) (*Universe, error) {
	if version == "" {
		return nil, fmt.Errorf("universe: version is empty")
	}
	u := &Universe{Version: version, Entries: entries, byTicker: map[string]Entry{}}
	owner := map[string]string{} // alias -> ticker
	for _, e := range entries {
		if !tickerPattern.MatchString(e.Ticker) {
			return nil, fmt.Errorf("universe: ticker %q must be 1-5 upper-case letters with an optional .X class", e.Ticker)
		}
		if _, dup := u.byTicker[e.Ticker]; dup {
			return nil, fmt.Errorf("universe: ticker %s listed twice", e.Ticker)
		}
		if e.Type != TypeEquity && e.Type != TypeETF {
			return nil, fmt.Errorf("universe: %s type %q, want %q or %q", e.Ticker, e.Type, TypeEquity, TypeETF)
		}
		if e.Type == TypeETF && len(e.Aliases) > 0 {
			return nil, fmt.Errorf("universe: ETF %s has aliases; fund names are not matched", e.Ticker)
		}
		for _, a := range e.Aliases {
			if a != strings.TrimSpace(a) || utf8.RuneCountInString(a) < minAliasRunes {
				return nil, fmt.Errorf("universe: %s alias %q must be at least %d characters with no surrounding space", e.Ticker, a, minAliasRunes)
			}
			if prev, dup := owner[a]; dup {
				return nil, fmt.Errorf("universe: alias %q listed for both %s and %s", a, prev, e.Ticker)
			}
			owner[a] = e.Ticker
			u.aliases = append(u.aliases, aliasEntry{alias: a, ticker: e.Ticker})
		}
		u.byTicker[e.Ticker] = e
	}
	// An alias that contains another ticker's alias as a phrase would credit both
	// companies for one name ("Example Bank Holdings" vs "Example Bank").
	for _, outer := range u.aliases {
		for _, inner := range u.aliases {
			if outer.ticker != inner.ticker && len(findPhrase(outer.alias, inner.alias)) > 0 {
				return nil, fmt.Errorf("universe: alias %q (%s) contains alias %q (%s)", outer.alias, outer.ticker, inner.alias, inner.ticker)
			}
		}
	}
	sort.SliceStable(u.aliases, func(i, j int) bool {
		if len(u.aliases[i].alias) != len(u.aliases[j].alias) {
			return len(u.aliases[i].alias) > len(u.aliases[j].alias)
		}
		return u.aliases[i].alias < u.aliases[j].alias
	})
	return u, nil
}

// Has reports whether ticker is allowlisted.
func (u *Universe) Has(ticker string) bool {
	_, ok := u.byTicker[ticker]
	return ok
}
