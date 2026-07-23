package weeklydeepdive

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/sl6117/automations/internal/agent"
	"github.com/sl6117/automations/pkg/sources"
)

// seedTweets resolves the planner's sourceTweetIDs back to full tweets by
// re-reading recent run artifacts through the same MCP tools the agents use.
// Plain code, zero LLM tokens: researchers get real source material instead
// of guessing URLs.
func seedTweets(ctx context.Context, tools agent.ToolSource, ids []string, since time.Time) ([]sources.Tweet, error) {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	args, err := json.Marshal(map[string]string{"since": since.Format("2006-01-02")})
	if err != nil {
		return nil, err
	}
	out, isErr, err := tools.Call(ctx, "list_runs", args)
	if err != nil {
		return nil, fmt.Errorf("list_runs: %w", err)
	}
	if isErr {
		return nil, fmt.Errorf("list_runs: %s", out)
	}
	var runs struct {
		Keys []string `json:"keys"`
	}
	if err := json.Unmarshal([]byte(out), &runs); err != nil {
		return nil, fmt.Errorf("parse list_runs: %w", err)
	}
	var seeds []sources.Tweet
	for _, key := range runs.Keys {
		if len(want) == 0 {
			break // all IDs resolved
		}
		args, err := json.Marshal(map[string]any{"key": key, "includeTweets": true})
		if err != nil {
			return nil, err
		}
		out, isErr, err := tools.Call(ctx, "get_artifact", args)
		if err != nil {
			return nil, fmt.Errorf("get_artifact %s: %w", key, err)
		}
		if isErr {
			return nil, fmt.Errorf("get_artifact %s: %s", key, out)
		}
		var got struct {
			Artifact struct {
				Kept []sources.Tweet `json:"kept"`
			} `json:"artifact"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			return nil, fmt.Errorf("parse artifact %s: %w", key, err)
		}
		for _, t := range got.Artifact.Kept {
			if want[t.ID] {
				seeds = append(seeds, t)
				delete(want, t.ID) // dedup: same tweet can appear in several runs
			}
		}
	}
	return seeds, nil
}

var urlPattern = regexp.MustCompile(`https?://\S+`)

// renderSeeds formats seed tweets as prompt material. Embedded links are
// pulled onto their own lines so the researcher fetches them instead of
// inventing URLs. Empty seeds render as "" (prompt degrades gracefully).
func renderSeeds(seeds []sources.Tweet) string {
	if len(seeds) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Source tweets behind this story (verbatim; fetch the embedded links):\n")
	for _, t := range seeds {
		fmt.Fprintf(&b, "- @%s: %s\n", t.Handle, t.Text)
		for _, u := range urlPattern.FindAllString(t.Text, -1) {
			fmt.Fprintf(&b, "  link: %s\n", strings.TrimRight(u, ".,;:!?)\""))
		}
	}
	b.WriteString("\n")
	return b.String()
}
