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
