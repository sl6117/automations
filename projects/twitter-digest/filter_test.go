package twitterdigest

import (
	"testing"

	"github.com/sl6117/automations/pkg/sources"
)

func observationFor(t *testing.T, obs []PostObservation, id string) PostObservation {
	t.Helper()
	for _, o := range obs {
		if o.ID == id {
			return o
		}
	}
	t.Fatalf("no observation for post %q; got %+v", id, obs)
	return PostObservation{}
}

func TestFilter(t *testing.T) {
	in := []sources.Tweet{
		{ID: "1", Text: "Real signal about AI", Likes: 500, Reposts: 100}, // keep
		{ID: "2", Text: "spam", Likes: 1, Reposts: 0},                     // drop: low engagement
		{ID: "3", Text: "Real signal about AI", Likes: 900, Reposts: 50},  // drop: duplicate of #1
		{ID: "4", Text: "Markets update", Likes: 300, Reposts: 80},        // keep
	}

	got, _ := filter(in, 100, 0)

	if len(got) != 2 {
		t.Fatalf("got %d tweets, want 2", len(got))
	}

	if got[0].ID != "1" || got[1].ID != "4" {
		t.Errorf("unexpected survivors: got IDS %q, %q; want 1,4", got[0].ID, got[1].ID)
	}
}

func TestFilterMaxPerAuthor(t *testing.T) {
	in := []sources.Tweet{
		{ID: "1", Handle: "@ylecun", Text: "story A", Likes: 500},
		{ID: "2", Handle: "@ylecun", Text: "story B", Likes: 500},
		{ID: "3", Handle: "@steipete", Text: "story C", Likes: 500},
		{ID: "4", Handle: "@ylecun", Text: "story D", Likes: 500}, // drop: over cap
		{ID: "5", Handle: "@steipete", Text: "story E", Likes: 500},
	}

	got, _ := filter(in, 100, 2) // max is 2

	wantIDs := []string{"1", "2", "3", "5"}

	if len(got) != len(wantIDs) {
		t.Fatalf("got %d tweets, want %d", len(got), len(wantIDs))
	}
	for i, id := range wantIDs {
		if got[i].ID != id {
			t.Errorf("survivor[%d] = %q, want %q", i, got[i].ID, id)
		}
	}
}

// Every post we paid to fetch gets a row, in fetch order. The rows are the dataset a
// scoring rubric is fitted against later, so a dropped post is as important as a kept one
// and the newest-first ordering has to survive.
func TestFilterObservesEveryFetchedPostInOrder(t *testing.T) {
	in := []sources.Tweet{
		{ID: "1", Handle: "@a", Text: "one", Likes: 500},
		{ID: "2", Handle: "@a", Text: "one", Likes: 500}, // duplicate
		{ID: "3", Handle: "@a", Text: "two", Likes: 0},   // low engagement
		{ID: "4", Handle: "@b", Text: "three", Likes: 500},
	}

	_, obs := filter(in, 100, 0)

	if len(obs) != len(in) {
		t.Fatalf("got %d observations, want %d (one per fetched post)", len(obs), len(in))
	}
	for i, tweet := range in {
		if obs[i].ID != tweet.ID {
			t.Errorf("observation[%d] = %q, want %q (fetch order must be preserved)", i, obs[i].ID, tweet.ID)
		}
	}
}

// Drop reasons are exclusive and follow the check order in filter: a post rejected for
// low engagement is never also recorded as a duplicate or over the cap.
func TestFilterRecordsExclusiveDropReasons(t *testing.T) {
	in := []sources.Tweet{
		{ID: "1", Handle: "@loud", Text: "story A", Likes: 500},
		{ID: "2", Handle: "@loud", Text: "story B", Likes: 500},
		{ID: "3", Handle: "@loud", Text: "story A", Likes: 500}, // duplicate of #1
		{ID: "4", Handle: "@loud", Text: "story C", Likes: 500}, // over the cap of 2
		{ID: "5", Handle: "@loud", Text: "story D", Likes: 3},   // low engagement
		{ID: "6", Handle: "@quiet", Text: "story E", Likes: 500},
	}

	kept, obs := filter(in, 100, 2)

	if len(kept) != 3 {
		t.Fatalf("kept %d, want 3 (1, 2, 6)", len(kept))
	}

	want := map[string]DropReason{
		"1": "",
		"2": "",
		"3": DropDuplicate,
		"4": DropPerAuthorCap,
		"5": DropLowEngagement,
		"6": "",
	}
	for id, reason := range want {
		o := observationFor(t, obs, id)
		if o.DropReason != reason {
			t.Errorf("post %s: dropReason = %q, want %q", id, o.DropReason, reason)
		}
		if o.Kept != (reason == "") {
			t.Errorf("post %s: kept = %v with reason %q", id, o.Kept, o.DropReason)
		}
	}
}

// A kept post carries no reason and a dropped post always carries one: the report cannot
// account for the fetch budget if a row is ambiguous.
func TestFilterObservationVerdictIsAlwaysDecided(t *testing.T) {
	in := []sources.Tweet{
		{ID: "1", Handle: "@a", Text: "kept", Likes: 500},
		{ID: "2", Handle: "@a", Text: "dropped", Likes: 1},
	}

	_, obs := filter(in, 100, 0)

	for _, o := range obs {
		if o.Kept && o.DropReason != "" {
			t.Errorf("post %s kept but carries reason %q", o.ID, o.DropReason)
		}
		if !o.Kept && o.DropReason == "" {
			t.Errorf("post %s dropped with no reason", o.ID)
		}
	}
}

// The row has to carry everything the fetch paid for, including on dropped posts:
// curation needs to see what a handle actually produces, not just what survived.
func TestFilterObservationsCarryTheMetricsWePaidFor(t *testing.T) {
	in := []sources.Tweet{
		{
			ID: "1", Handle: "@a", Text: "dropped but recorded",
			Likes: 2, Reposts: 1, Replies: 12, Quotes: 7,
			Bookmarks: 63, Impressions: 91000, AuthorFollowers: 250000,
		},
	}

	_, obs := filter(in, 100, 0)

	if len(obs) != 1 {
		t.Fatalf("got %d observations, want 1", len(obs))
	}
	o := obs[0]
	if o.Kept {
		t.Fatalf("post should have been dropped for low engagement: %+v", o)
	}
	if o.Handle != "@a" {
		t.Errorf("handle = %q, want @a", o.Handle)
	}
	if o.Likes != 2 || o.Reposts != 1 {
		t.Errorf("likes/reposts = %d/%d, want 2/1", o.Likes, o.Reposts)
	}
	if o.Replies != 12 || o.Quotes != 7 {
		t.Errorf("replies/quotes = %d/%d, want 12/7", o.Replies, o.Quotes)
	}
	if o.Bookmarks != 63 || o.Impressions != 91000 {
		t.Errorf("bookmarks/impressions = %d/%d, want 63/91000", o.Bookmarks, o.Impressions)
	}
	if o.AuthorFollowers != 250000 {
		t.Errorf("authorFollowers = %d, want 250000", o.AuthorFollowers)
	}
}

// The feed arrives newest-first, so a first-come cap keeps an author's most recent posts
// whatever their quality. The cap decides ~30% of a real fetch, so it has to rank the
// author's eligible posts and keep the best ones.
func TestFilterCapKeepsHighestEngagementNotNewest(t *testing.T) {
	in := []sources.Tweet{
		{ID: "1", Handle: "@loud", Text: "newest but thin", Likes: 100, Reposts: 5},
		{ID: "2", Handle: "@loud", Text: "also thin", Likes: 110, Reposts: 2},
		{ID: "3", Handle: "@loud", Text: "the story of the day", Likes: 4000, Reposts: 900},
		{ID: "4", Handle: "@loud", Text: "strong second", Likes: 2500, Reposts: 300},
	}

	kept, obs := filter(in, 100, 2)

	wantIDs := []string{"3", "4"}
	if len(kept) != len(wantIDs) {
		t.Fatalf("kept %d, want %d", len(kept), len(wantIDs))
	}
	for i, id := range wantIDs {
		if kept[i].ID != id {
			t.Errorf("survivor[%d] = %q, want %q", i, kept[i].ID, id)
		}
	}
	for _, id := range []string{"1", "2"} {
		if o := observationFor(t, obs, id); o.DropReason != DropPerAuthorCap {
			t.Errorf("post %s: dropReason = %q, want %q", id, o.DropReason, DropPerAuthorCap)
		}
	}
}

// Ranking decides which posts survive the cap, not what order the model reads them in.
// The digest prompt is fed newest-first; reordering it by score is a silent prompt change.
func TestFilterCapPreservesFetchOrderOfSurvivors(t *testing.T) {
	in := []sources.Tweet{
		{ID: "1", Handle: "@a", Text: "strong", Likes: 3000},
		{ID: "2", Handle: "@a", Text: "weak", Likes: 150},
		{ID: "3", Handle: "@a", Text: "strongest", Likes: 5000},
	}

	kept, _ := filter(in, 100, 2)

	wantIDs := []string{"1", "3"}
	if len(kept) != len(wantIDs) {
		t.Fatalf("kept %d, want %d", len(kept), len(wantIDs))
	}
	for i, id := range wantIDs {
		if kept[i].ID != id {
			t.Errorf("survivor[%d] = %q, want %q (fetch order, not score order)", i, kept[i].ID, id)
		}
	}
}

// Equal engagement falls back to the old behaviour: the newer post wins. Without a stable
// rank the kept set would drift between runs on identical input.
func TestFilterCapTieBreaksTowardNewer(t *testing.T) {
	in := []sources.Tweet{
		{ID: "1", Handle: "@a", Text: "story A", Likes: 400, Reposts: 100},
		{ID: "2", Handle: "@a", Text: "story B", Likes: 500},
		{ID: "3", Handle: "@a", Text: "story C", Likes: 500},
	}

	kept, _ := filter(in, 100, 2)

	wantIDs := []string{"1", "2"}
	if len(kept) != len(wantIDs) {
		t.Fatalf("kept %d, want %d", len(kept), len(wantIDs))
	}
	for i, id := range wantIDs {
		if kept[i].ID != id {
			t.Errorf("survivor[%d] = %q, want %q", i, kept[i].ID, id)
		}
	}
}

// A post already rejected for low engagement or as a duplicate must not burn a cap slot:
// the cap is a budget over posts the model could actually have been shown.
func TestFilterCapCountsOnlyEligiblePosts(t *testing.T) {
	in := []sources.Tweet{
		{ID: "1", Handle: "@a", Text: "spam", Likes: 1},
		{ID: "2", Handle: "@a", Text: "story A", Likes: 500},
		{ID: "3", Handle: "@a", Text: "story A", Likes: 900},
		{ID: "4", Handle: "@a", Text: "story B", Likes: 400},
		{ID: "5", Handle: "@a", Text: "story C", Likes: 300},
	}

	kept, obs := filter(in, 100, 3)

	wantIDs := []string{"2", "4", "5"}
	if len(kept) != len(wantIDs) {
		t.Fatalf("kept %d, want %d", len(kept), len(wantIDs))
	}
	for i, id := range wantIDs {
		if kept[i].ID != id {
			t.Errorf("survivor[%d] = %q, want %q", i, kept[i].ID, id)
		}
	}
	if o := observationFor(t, obs, "1"); o.DropReason != DropLowEngagement {
		t.Errorf("post 1: dropReason = %q, want %q", o.DropReason, DropLowEngagement)
	}
	if o := observationFor(t, obs, "3"); o.DropReason != DropDuplicate {
		t.Errorf("post 3: dropReason = %q, want %q", o.DropReason, DropDuplicate)
	}
}

// X reports a retweet with zero likes and the ORIGINAL post's repost count, so raw
// engagement scores a retweet by someone else's audience. Left alone it wins cap slots off
// the author's own reporting: on 2026-07-29 a retweeted inspirational quote scored 2498 and
// took a slot from @elonmusk's real posts.
func TestFilterCapRanksRetweetsBelowOriginals(t *testing.T) {
	in := []sources.Tweet{
		{ID: "1", Handle: "@elon", Text: "RT @someone: an inspirational quote", Reposts: 2498},
		{ID: "2", Handle: "@elon", Text: "Starship static fire complete", Likes: 400, Reposts: 60},
		{ID: "3", Handle: "@elon", Text: "Neuralink update", Likes: 300, Reposts: 40},
	}

	kept, obs := filter(in, 100, 2)

	wantIDs := []string{"2", "3"}
	if len(kept) != len(wantIDs) {
		t.Fatalf("kept %d, want %d", len(kept), len(wantIDs))
	}
	for i, id := range wantIDs {
		if kept[i].ID != id {
			t.Errorf("survivor[%d] = %q, want %q (originals outrank the retweet)", i, kept[i].ID, id)
		}
	}
	if o := observationFor(t, obs, "1"); o.DropReason != DropPerAuthorCap {
		t.Errorf("retweet: dropReason = %q, want %q", o.DropReason, DropPerAuthorCap)
	}
}

// Demoting retweets must not mean excluding them: real news arrives by retweet, and the
// 2026-07-29 digest's whole AI section came from one. A retweet takes a slot whenever the
// author has fewer originals than the cap, and retweets rank against each other on engagement.
func TestFilterCapKeepsBestRetweetWhenAuthorLacksOriginals(t *testing.T) {
	in := []sources.Tweet{
		{ID: "1", Handle: "@elon", Text: "RT @cb_doge: Grok 4.5 ranked #1 on LaurenBench", Reposts: 145},
		{ID: "2", Handle: "@elon", Text: "RT @someone: an inspirational quote", Reposts: 2498},
		{ID: "3", Handle: "@elon", Text: "Starship static fire complete", Likes: 400, Reposts: 60},
	}

	kept, obs := filter(in, 100, 2)

	wantIDs := []string{"2", "3"}
	if len(kept) != len(wantIDs) {
		t.Fatalf("kept %d, want %d", len(kept), len(wantIDs))
	}
	for i, id := range wantIDs {
		if kept[i].ID != id {
			t.Errorf("survivor[%d] = %q, want %q", i, kept[i].ID, id)
		}
	}
	if o := observationFor(t, obs, "1"); o.DropReason != DropPerAuthorCap {
		t.Errorf("weaker retweet: dropReason = %q, want %q", o.DropReason, DropPerAuthorCap)
	}
}

// The demotion lives in the cap, not the floor: a retweet with no rival for a slot is kept.
func TestFilterKeepsRetweetsThatClearTheFloor(t *testing.T) {
	in := []sources.Tweet{
		{ID: "1", Handle: "@a", Text: "RT @b: genuinely breaking news", Reposts: 500},
	}

	kept, _ := filter(in, 100, 0)

	if len(kept) != 1 || kept[0].ID != "1" {
		t.Errorf("kept = %+v, want the retweet: the cap demotes retweets, the floor does not", kept)
	}
}
