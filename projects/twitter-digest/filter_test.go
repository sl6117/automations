package twitterdigest

import (
	"testing"

	"github.com/sl6117/automations/pkg/sources"
)

func TestFilter(t *testing.T) {
	in := []sources.Tweet{
		{ID: "1", Text: "Real signal about AI", Likes: 500, Reposts: 100}, // keep
		{ID: "2", Text: "spam", Likes: 1, Reposts: 0},                     // drop: low engagement
		{ID: "3", Text: "Real signal about AI", Likes: 900, Reposts: 50},  // drop: duplicate of #1
		{ID: "4", Text: "Markets update", Likes: 300, Reposts: 80},        // keep
	}

	got := filter(in, 100)

	if len(got) != 2 {
		t.Fatalf("got %d tweets, want 2", len(got))
	}

	if got[0].ID != "1" || got[1].ID != "4" {
		t.Errorf("unexpected survivors: got IDS %q, %q; want 1,4", got[0].ID, got[1].ID)
	}
}
