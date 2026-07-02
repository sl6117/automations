package twitterdigest

import (
	"testing"

	"github.com/sl6117/automations/pkg/sources"
)

func TestSummarize(t *testing.T) {
	in := []sources.Tweet{
		{ID: "1", Text: "New AI model released by OpenAI"},
		{ID: "2", Text: "Crypto markets rally"},
		{ID: "3", Text: "random thoughts"},
	}
	topics := []Topic{{Name: "AI"}, {Name: "Crypto"}}

	d := summarize(in, topics)

	if len(d.Buckets) != 3 {
		t.Fatalf("expected 3 buckets, got %d: %+v", len(d.Buckets), d.Buckets)
	}

	if d.Buckets[0].Topic != "AI" || len(d.Buckets[0].Tweets) != 1 {
		t.Errorf("AI bucket wrong: %+v", d.Buckets[0])
	}
	if d.Buckets[2].Topic != "Other" || len(d.Buckets[2].Tweets) != 1 {
		t.Errorf("Other bucket wrong: %+v", d.Buckets[2])
	}
}
