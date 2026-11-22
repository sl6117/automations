package kf

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sl6117/automations/internal/corpus"
)

var today = time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)

func testUniverse(t *testing.T) *Universe {
	t.Helper()
	u, err := NewUniverse("test", []Entry{
		{Ticker: "EXMPL", Type: TypeEquity, Aliases: []string{"Example Corp"}},
		{Ticker: "EXMPB", Type: TypeEquity},
		{Ticker: "EXMPA", Type: TypeEquity},
		{Ticker: "EXMPA.B", Type: TypeEquity},
		{Ticker: "EXETF", Type: TypeETF},
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// at returns a time on the given day offset from today, so posts land in that partition.
func at(dayOffset int) time.Time { return today.AddDate(0, 0, dayOffset).Add(16 * time.Hour) }

func own(id, handle, text string) corpus.StoredPost {
	return corpus.StoredPost{ID: id, Handle: handle, Text: text, OwnText: text, FetchedAt: at(0)}
}

func retweet(id, handle, origID, origHandle, text string) corpus.StoredPost {
	return corpus.StoredPost{ID: id, Handle: handle, Text: "RT " + origHandle + ": " + text, FetchedAt: at(0),
		Refs: []corpus.Ref{{Type: "retweeted", ID: origID, AuthorHandle: origHandle, Text: text}}}
}

func cashtagsIn(u *Universe, text string) []string {
	var out []string
	for _, h := range u.scanText(text, InOwn, -1, "1") {
		if h.Kind == KindCashtag {
			out = append(out, h.Ticker+"="+h.Surface)
		}
	}
	return out
}

func TestCashtagMatching(t *testing.T) {
	u := testUniverse(t)
	cases := []struct {
		text string
		want []string
	}{
		{"$EXMPL", []string{"EXMPL=$EXMPL"}},
		{"buying $exmpl today", []string{"EXMPL=$exmpl"}},
		{"($EXMPL)", []string{"EXMPL=$EXMPL"}},
		{"$EXMPL, then", []string{"EXMPL=$EXMPL"}},
		{"$EXMPL's guidance", []string{"EXMPL=$EXMPL"}},
		{"ends with $EXMPL.", []string{"EXMPL=$EXMPL"}},
		{"$$EXMPL", []string{"EXMPL=$EXMPL"}},
		{"$100 target", nil},
		{"costs US$5", nil},
		{"A$EXMPL", nil},
		{"1$EXMPL", nil},
		{"é$EXMPL", nil}, // unicode letters block too
		{"$EXMPLX1", nil},
		{"$EXMPLEXTRA", nil},
		{"$EXMPLé", nil},
		{"$BTC pumps", nil}, // not in the universe
		// a class suffix is matched whole and never also as its prefix
		{"$EXMPA.B up", []string{"EXMPA.B=$EXMPA.B"}},
		{"$EXMPA up", []string{"EXMPA=$EXMPA"}},
		// an unlisted class reads as punctuation when the base is listed
		{"$EXMPL.A great quarter", []string{"EXMPL=$EXMPL"}},
		{"$EXMPL.Next", []string{"EXMPL=$EXMPL"}}, // a dot then a word is punctuation, not a class
		{"$EXMPL and $EXMPL again", []string{"EXMPL=$EXMPL"}},
		{"$EXMPL $EXMPB", []string{"EXMPL=$EXMPL", "EXMPB=$EXMPB"}},
	}
	for _, tc := range cases {
		if got := cashtagsIn(u, tc.text); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: cashtags = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestAliasMatching(t *testing.T) {
	u := testUniverse(t)
	cases := []struct {
		text string
		want bool
	}{
		{"Example Corp beat estimates", true},
		{"(Example Corp)", true},
		{"Example Corp's margins", true},
		{"rumours about Example Corp.", true},
		{"Example Corporation", false}, // inside a longer word
		{"SuperExample Corp", false},
		{"Example Corp1", false},
		{"example corp", false}, // case-sensitive
		{"EXAMPLE CORP", false},
		{"cc @Example Corp", false}, // a handle, not the company in prose
		{"ÜExample Corp", false},
	}
	for _, tc := range cases {
		hits := u.scanText(tc.text, InOwn, -1, "1")
		got := len(hits) == 1 && hits[0].Kind == KindAlias && hits[0].Ticker == "EXMPL" && hits[0].Surface == "Example Corp"
		if got != tc.want || (!tc.want && len(hits) != 0) {
			t.Errorf("%q: hits = %+v, want alias match %v", tc.text, hits, tc.want)
		}
	}
}

func TestMentionsSeparateOwnWordsFromReferencedPosts(t *testing.T) {
	u := testUniverse(t)
	quote := own("1900000000000000003", "@example_wire", "Agree on $EXMPL")
	quote.Text = "Agree on $EXMPL\n[quoting @example_desk: $EXMPL and Example Corp]"
	quote.Refs = []corpus.Ref{{Type: "quoted", ID: "1900000000000000013", AuthorHandle: "@example_desk", Text: "$EXMPL and Example Corp"}}
	multi := own("1900000000000000004", "@example_wire", "two refs")
	multi.Refs = []corpus.Ref{
		{Type: "quoted", ID: "1900000000000000014", Text: "$EXMPB first"},
		{Type: "replied_to", ID: "1900000000000000015", Text: "$EXMPB second"},
	}
	flattenedOnly := own("1900000000000000005", "@example_wire", "nothing here")
	flattenedOnly.Text = "nothing here $EXMPB" // Text is never scanned
	posts := []corpus.StoredPost{
		multi, flattenedOnly, quote,
		retweet("1900000000000000002", "@example_wire", "1900000000000000012", "@example_desk", "$EXMPL ripping"),
	}
	got := Mentions(u, posts)
	want := []Mention{
		{PostID: "1900000000000000002", Ticker: "EXMPL", Kind: KindCashtag, In: InRef, Surface: "$EXMPL"},
		{PostID: "1900000000000000003", Ticker: "EXMPL", Kind: KindAlias, In: InRef, Surface: "Example Corp"},
		{PostID: "1900000000000000003", Ticker: "EXMPL", Kind: KindCashtag, In: InOwn, Surface: "$EXMPL"},
		{PostID: "1900000000000000003", Ticker: "EXMPL", Kind: KindCashtag, In: InRef, Surface: "$EXMPL"},
		{PostID: "1900000000000000004", Ticker: "EXMPB", Kind: KindCashtag, In: InRef, Surface: "$EXMPB"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Mentions =\n%+v\nwant\n%+v", got, want)
	}
}

func TestEntityDayCountsPostsAndVoices(t *testing.T) {
	u := testUniverse(t)
	quote := own("1900000000000000003", "@example_wire", "Agree on $EXMPL")
	quote.Refs = []corpus.Ref{{Type: "quoted", ID: "1900000000000000013", AuthorHandle: "@example_desk", Text: "$EXMPL looks cheap"}}
	posts := []corpus.StoredPost{
		retweet("1900000000000000001", "@example_one", "1900000000000000010", "@example_orig", "$EXMPB to the moon"),
		retweet("1900000000000000002", "@example_two", "1900000000000000010", "@Example_Orig", "$EXMPB to the moon"),
		retweet("1900000000000000004", "@example_six", "1900000000000000010", "@example_orig", "$EXMPB to the moon"),
		quote,
		own("1900000000000000005", "@example_wire", "Example Corp only by name"),
		own("1900000000000000006", "@example_wire", "no ticker"),
	}
	rows := EntityDays(u, Day{Date: today, Posts: posts, Runs: []corpus.RunRecord{{FetchedAt: at(0), Fetched: 6, Written: 6}}}, nil)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want EXMPB and EXMPL", rows)
	}
	b, l := rows[0], rows[1]
	if b.Ticker != "EXMPB" || b.Posts != 3 || b.UniqueVoices != 1 || b.PostsOwn != 0 || b.PostsCashtag != 3 {
		t.Errorf("EXMPB = %+v, want 3 posts, 1 voice (the original author), 0 own, 3 cashtag", b)
	}
	// the quote mentions it in both own and ref but is one post; voices are the quoter and the quoted
	if l.Ticker != "EXMPL" || l.Posts != 2 || l.PostsOwn != 2 || l.PostsCashtag != 1 || l.UniqueVoices != 2 {
		t.Errorf("EXMPL = %+v, want 2 posts, 2 own, 1 cashtag, 2 voices", l)
	}
	if b.PostsFetched != 6 || math.Abs(b.Share-0.5) > 1e-12 || b.Date != "2026-11-21" || b.SchemaVersion != "kf.entity_day.v1" {
		t.Errorf("EXMPB denominator/share/date = %d/%v/%s, want 6/0.5/2026-11-21", b.PostsFetched, b.Share, b.Date)
	}
	if !b.AvailableAt.Equal(at(0)) {
		t.Errorf("AvailableAt = %v, want the run's fetch time", b.AvailableAt)
	}
}

// An unknown referenced author is still one voice per original post, not zero and not one
// for every unknown author together.
func TestVoiceFallsBackToReferencedPostID(t *testing.T) {
	u := testUniverse(t)
	a := retweet("1900000000000000001", "@example_one", "1900000000000000010", "", "$EXMPB")
	b := retweet("1900000000000000002", "@example_two", "1900000000000000011", "", "$EXMPB")
	c := retweet("1900000000000000003", "@example_three", "1900000000000000011", "", "$EXMPB")
	rows := EntityDays(u, Day{Date: today, Posts: []corpus.StoredPost{a, b, c}}, nil)
	if len(rows) != 1 || rows[0].UniqueVoices != 2 {
		t.Errorf("rows = %+v, want 2 voices (two distinct original posts)", rows)
	}
}

// history builds days before today where `mentioned` of `fetched` posts carry $EXMPL.
func historyDay(offset, fetched, mentioned int, withRun bool) Day {
	d := Day{Date: today.AddDate(0, 0, offset)}
	for i := 0; i < fetched; i++ {
		text := "nothing"
		if i < mentioned {
			text = "$EXMPL"
		}
		p := own(fmt.Sprintf("18%02d%016d", -offset, i), "@example_wire", text)
		p.FetchedAt = at(offset)
		d.Posts = append(d.Posts, p)
	}
	if withRun {
		d.Runs = []corpus.RunRecord{{FetchedAt: at(offset), Fetched: fetched, Written: fetched}}
	}
	return d
}

func todayWith(mentioned, fetched int) Day {
	d := historyDay(0, fetched, mentioned, true)
	return d
}

func exmplRow(t *testing.T, rows []EntityDay) EntityDay {
	t.Helper()
	for _, r := range rows {
		if r.Ticker == "EXMPL" {
			return r
		}
	}
	t.Fatalf("no EXMPL row in %+v", rows)
	return EntityDay{}
}

func TestBaseline(t *testing.T) {
	u := testUniverse(t)
	series := []int{1, 2, 3, 1, 2, 3, 1, 2, 3, 2} // of 10 posts per day: mean 0.2, sample std sqrt(0.06/9)

	t.Run("known series", func(t *testing.T) {
		var hist []Day
		for i, k := range series {
			hist = append(hist, historyDay(-(i+1), 10, k, true))
		}
		hist = append(hist, historyDay(-15, 10, 10, true)) // outside the 14-day window
		r := exmplRow(t, EntityDays(u, todayWith(5, 10), hist))
		wantStd := math.Sqrt(0.06 / 9)
		if r.Baseline.Days != 10 || math.Abs(r.Baseline.MeanShare-0.2) > 1e-12 || math.Abs(r.Baseline.StdShare-wantStd) > 1e-12 {
			t.Errorf("baseline = %+v, want 10 days, mean 0.2, std %v", r.Baseline, wantStd)
		}
		if r.ZShare == nil || math.Abs(*r.ZShare-(0.5-0.2)/wantStd) > 1e-9 {
			t.Errorf("z = %v, want %v", r.ZShare, (0.5-0.2)/wantStd)
		}
	})
	t.Run("fewer than ten data days", func(t *testing.T) {
		var hist []Day
		for i, k := range series[:9] {
			hist = append(hist, historyDay(-(i+1), 10, k, true))
		}
		r := exmplRow(t, EntityDays(u, todayWith(5, 10), hist))
		if r.Baseline.Days != 9 || r.ZShare != nil {
			t.Errorf("baseline=%+v z=%v, want 9 days and a null z", r.Baseline, r.ZShare)
		}
	})
	t.Run("a day missing from the corpus is not a zero", func(t *testing.T) {
		var hist []Day
		for i, k := range series[:9] {
			hist = append(hist, historyDay(-(i+1), 10, k, true))
		}
		// posts but no run record: not a data day, whatever it holds
		hist = append(hist, historyDay(-10, 10, 0, false))
		r := exmplRow(t, EntityDays(u, todayWith(5, 10), hist))
		wantMean := 0.0
		for _, k := range series[:9] {
			wantMean += float64(k) / 10 / 9
		}
		if r.Baseline.Days != 9 || math.Abs(r.Baseline.MeanShare-wantMean) > 1e-12 || r.ZShare != nil {
			t.Errorf("baseline=%+v z=%v, want 9 days, mean %v, null z", r.Baseline, r.ZShare, wantMean)
		}
	})
	t.Run("a data day without the ticker counts as zero", func(t *testing.T) {
		var hist []Day
		for i := 1; i <= 12; i++ {
			k := 0
			if i == 1 {
				k = 1
			}
			hist = append(hist, historyDay(-i, 10, k, true))
		}
		r := exmplRow(t, EntityDays(u, todayWith(5, 10), hist))
		if r.Baseline.Days != 12 || math.Abs(r.Baseline.MeanShare-0.1/12) > 1e-12 || r.ZShare == nil {
			t.Errorf("baseline=%+v z=%v, want 12 days, mean %v, a z", r.Baseline, r.ZShare, 0.1/12)
		}
	})
	t.Run("flat history", func(t *testing.T) {
		var hist []Day
		for i := 1; i <= 14; i++ {
			// 0.3 summed 14 times does not divide back to exactly 0.3: the naive std is ~6e-17
			hist = append(hist, historyDay(-i, 10, 3, true))
		}
		r := exmplRow(t, EntityDays(u, todayWith(5, 10), hist))
		if r.Baseline.Days != 14 || r.Baseline.StdShare != 0 || r.Baseline.MeanShare != 0.3 || r.ZShare != nil {
			t.Errorf("baseline=%+v z=%v, want std exactly 0 and a null z", r.Baseline, r.ZShare)
		}
	})
}

func TestWindowFlags(t *testing.T) {
	u := testUniverse(t)
	posts := []corpus.StoredPost{own("1900000000000000001", "@example_wire", "$EXMPL")}
	cases := []struct {
		name               string
		runs               []corpus.RunRecord
		truncated, partial bool
	}{
		{"clean", []corpus.RunRecord{{FetchedAt: at(0), Fetched: 1, Written: 1}}, false, false},
		{"truncated", []corpus.RunRecord{{FetchedAt: at(0), Fetched: 1, Written: 1}, {FetchedAt: at(0).Add(time.Hour), Truncated: true, Fetched: 1, Written: 1}}, true, false},
		{"partial", []corpus.RunRecord{{FetchedAt: at(0), Fetched: 2, Written: 1}}, false, true},
		{"other day's run ignored", []corpus.RunRecord{{FetchedAt: at(-1), Truncated: true, Fetched: 2, Written: 0}}, false, false},
	}
	for _, tc := range cases {
		rows := EntityDays(u, Day{Date: today, Posts: posts, Runs: tc.runs}, nil)
		if len(rows) != 1 || rows[0].WindowTruncated != tc.truncated || rows[0].WindowPartial != tc.partial {
			t.Errorf("%s: rows = %+v, want truncated=%v partial=%v", tc.name, rows, tc.truncated, tc.partial)
		}
	}
	rows := EntityDays(u, Day{Date: today, Posts: posts, Runs: []corpus.RunRecord{{FetchedAt: at(0)}, {FetchedAt: at(0).Add(2 * time.Hour)}}}, nil)
	if !rows[0].AvailableAt.Equal(at(0).Add(2 * time.Hour)) {
		t.Errorf("AvailableAt = %v, want the latest run", rows[0].AvailableAt)
	}
}

func TestBackfilledPostsCountNowhere(t *testing.T) {
	u := testUniverse(t)
	bf := own("1900000000000000002", "@example_wire", "$EXMPL $NOTLISTED")
	bf.Backfilled = true
	posts := []corpus.StoredPost{own("1900000000000000001", "@example_wire", "plain"), bf}
	if m := Mentions(u, posts); len(m) != 0 {
		t.Errorf("Mentions = %+v, want none from a backfilled post", m)
	}
	if un := UnmappedCashtags(u, posts); len(un) != 0 {
		t.Errorf("Unmapped = %+v, want none from a backfilled post", un)
	}
	if rows := EntityDays(u, Day{Date: today, Posts: posts}, nil); len(rows) != 0 {
		t.Errorf("rows = %+v, want none", rows)
	}
	hist := historyDay(-1, 1, 1, true)
	hist.Posts[0].Backfilled = true
	rows := EntityDays(u, todayWith(1, 1), []Day{hist})
	if r := exmplRow(t, rows); r.Baseline.Days != 1 || r.Baseline.MeanShare != 0 {
		t.Errorf("baseline = %+v, want the backfilled history post ignored (share 0)", r.Baseline)
	}
}

func TestUnmappedCashtags(t *testing.T) {
	u := testUniverse(t)
	a := own("1", "@example_wire", "$BTC $BTC $ZZZ")
	b := own("2", "@example_wire", "$EXMPL")
	b.Refs = []corpus.Ref{{Text: "$btc and $QQQQ.Z"}}
	c := own("3", "@example_wire", "$ZZZ")
	got := UnmappedCashtags(u, []corpus.StoredPost{c, b, a})
	want := []UnmappedCashtag{{"BTC", 2}, {"ZZZ", 2}, {"QQQQ.Z", 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Unmapped = %+v, want %+v", got, want)
	}
}

func TestOutputIsByteIdenticalForShuffledInput(t *testing.T) {
	u := testUniverse(t)
	var posts []corpus.StoredPost
	for i := 0; i < 40; i++ {
		texts := []string{"$EXMPL", "Example Corp", "$EXMPB $EXETF", "$NOPE", "plain", "$EXMPA.B"}
		p := own(fmt.Sprintf("19000000000000%05d", i), fmt.Sprintf("@example_%d", i%7), texts[i%len(texts)])
		if i%3 == 0 {
			p.Refs = []corpus.Ref{{Type: "quoted", ID: fmt.Sprintf("17%d", i%5), AuthorHandle: "@example_src", Text: texts[(i+1)%len(texts)]}}
		}
		posts = append(posts, p)
	}
	var hist []Day
	for i := 1; i <= 14; i++ {
		hist = append(hist, historyDay(-i, 10, i%4, true))
	}
	render := func(ps []corpus.StoredPost, h []Day) string {
		out, err := json.Marshal(struct {
			M []Mention
			E []EntityDay
			U []UnmappedCashtag
		}{Mentions(u, ps), EntityDays(u, Day{Date: today, Posts: ps, Runs: []corpus.RunRecord{{FetchedAt: at(0), Fetched: len(ps), Written: len(ps)}}}, h), UnmappedCashtags(u, ps)})
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	want := render(posts, hist)
	rng := rand.New(rand.NewSource(1))
	for n := 0; n < 5; n++ {
		ps := append([]corpus.StoredPost(nil), posts...)
		ps = append(ps, posts[n]) // a duplicate fetch of the same post
		rng.Shuffle(len(ps), func(i, j int) { ps[i], ps[j] = ps[j], ps[i] })
		h := append([]Day(nil), hist...)
		rng.Shuffle(len(h), func(i, j int) { h[i], h[j] = h[j], h[i] })
		if got := render(ps, h); got != want {
			t.Fatalf("shuffle %d changed the output:\n%s\nwant\n%s", n, got, want)
		}
	}
	if !strings.Contains(want, `"z_share":`) {
		t.Errorf("z_share must always be present (null or a number): %s", want)
	}
}

func TestUniverseValidation(t *testing.T) {
	cases := []struct {
		name    string
		entries []Entry
	}{
		{"lower-case ticker", []Entry{{Ticker: "exmpl", Type: TypeEquity}}},
		{"too long", []Entry{{Ticker: "EXMPLE", Type: TypeEquity}}},
		{"duplicate ticker", []Entry{{Ticker: "EXMPL", Type: TypeEquity}, {Ticker: "EXMPL", Type: TypeETF}}},
		{"bad type", []Entry{{Ticker: "EXMPL", Type: "crypto"}}},
		{"short alias", []Entry{{Ticker: "EXMPL", Type: TypeEquity, Aliases: []string{"Exa"}}}},
		{"padded alias", []Entry{{Ticker: "EXMPL", Type: TypeEquity, Aliases: []string{" Example"}}}},
		{"shared alias", []Entry{{Ticker: "EXMPL", Type: TypeEquity, Aliases: []string{"Example Corp"}}, {Ticker: "EXMPB", Type: TypeEquity, Aliases: []string{"Example Corp"}}}},
		{"nested alias", []Entry{{Ticker: "EXMPL", Type: TypeEquity, Aliases: []string{"Example Bank"}}, {Ticker: "EXMPB", Type: TypeEquity, Aliases: []string{"Example Bank Holdings"}}}},
		{"etf alias", []Entry{{Ticker: "EXETF", Type: TypeETF, Aliases: []string{"Example Fund"}}}},
	}
	for _, tc := range cases {
		if _, err := NewUniverse("v", tc.entries); err == nil {
			t.Errorf("%s: accepted, want a validation error", tc.name)
		}
	}
	if _, err := NewUniverse("", nil); err == nil {
		t.Error("empty version accepted")
	}
}

func TestShippedUniverseIsValid(t *testing.T) {
	u, err := LoadUniverse("../../projects/knowledge-factory/universe.json")
	if err != nil {
		t.Fatalf("shipped universe invalid: %v", err)
	}
	if u.Version == "" || len(u.Entries) < 50 {
		t.Errorf("version %q with %d entries, want a versioned list of at least 50", u.Version, len(u.Entries))
	}
	// crypto stays out by construction: nothing here may be added as an equity
	for _, sym := range []string{"BTC", "ETH", "SOL", "DOGE", "XRP"} {
		if u.Has(sym) {
			t.Errorf("universe lists %s; feed v1 is equities and ETFs only", sym)
		}
	}
	// aliases that are ordinary words, first names or other well-known entities
	deny := []string{"Apple", "Amazon", "Meta", "Alphabet", "Google", "Target", "Oracle", "Visa", "Tesla",
		"Ford", "Intel", "Chevron", "Micron", "Snowflake", "Lucid", "Caterpillar", "Block", "Square",
		"Shell", "Gap", "Nike", "Disney", "Uber", "Reddit", "Palantir", "Merck", "Abbott", "Robinhood"}
	for _, e := range u.Entries {
		for _, a := range e.Aliases {
			for _, d := range deny {
				if strings.EqualFold(a, d) {
					t.Errorf("%s alias %q is ambiguous in running text", e.Ticker, a)
				}
			}
		}
	}
}
