package weeklydeepdive

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/pkg/sources"
)

type fakeTools struct {
	call func(name string, args json.RawMessage) (string, bool, error)
}

func (f fakeTools) Tools(ctx context.Context) ([]ai.ToolDef, error) { return nil, nil }
func (f fakeTools) Call(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	return f.call(name, args)
}
func TestSeedTweetsResolvesIDsAcrossArtifacts(t *testing.T) {
	tools := fakeTools{call: func(name string, args json.RawMessage) (string, bool, error) {
		switch name {
		case "list_runs":
			return `{"keys":["logs/runs/a.json","logs/runs/b.json"]}`, false, nil
		case "get_artifact":
			var in struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				t.Fatal(err)
			}
			if in.Key == "logs/runs/a.json" {
				return `{"artifact":{"kept":[{"ID":"1","Handle":"alice","Text":"breaking https://example.com/story"},{"ID":"2","Handle":"bob","Text":"noise"}]}}`, false, nil
			}
			return `{"artifact":{"kept":[{"ID":"3","Handle":"carol","Text":"more"},{"ID":"1","Handle":"alice","Text":"dupe"}]}}`, false, nil
		}
		t.Fatalf("unexpected tool %s", name)
		return "", false, nil
	}}
	seeds, err := seedTweets(context.Background(), tools, []string{"1", "3"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(seeds) != 2 || seeds[0].ID != "1" || seeds[1].ID != "3" {
		t.Fatalf("seeds = %+v, want IDs 1 and 3 once each", seeds)
	}
	if seeds[0].Text == "dupe" {
		t.Fatal("duplicate ID must keep the first occurrence")
	}
}
func TestSeedTweetsToolErrorFails(t *testing.T) {
	tools := fakeTools{call: func(name string, args json.RawMessage) (string, bool, error) {
		return "store unavailable", true, nil
	}}
	if _, err := seedTweets(context.Background(), tools, []string{"1"}, time.Now()); err == nil {
		t.Fatal("want error when the tool reports isError")
	}
}
func TestRenderSeeds(t *testing.T) {
	if got := renderSeeds(nil); got != "" {
		t.Fatalf("no seeds should render empty, got %q", got)
	}
	got := renderSeeds([]sources.Tweet{{Handle: "alice", Text: "read this https://example.com/story."}})
	if !strings.Contains(got, "@alice") || !strings.Contains(got, "read this") {
		t.Fatalf("missing tweet content: %q", got)
	}
	if !strings.Contains(got, "link: https://example.com/story\n") {
		t.Fatalf("link should be extracted with trailing punctuation stripped: %q", got)
	}
}
