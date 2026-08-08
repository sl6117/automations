package weeklydeepdive

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/sl6117/automations/internal/agent"
)

const (
	ragTopK     = 3
	ragMaxChars = 2000
)

var ragStop = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "of": true,
	"to": true, "in": true, "on": true, "for": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "as": true, "at": true, "by": true,
	"with": true, "from": true, "that": true, "this": true, "it": true,
}

type scoredChunk struct {
	Text  string
	Score int
}

func tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	var out []string
	for _, f := range fields {
		if len(f) < 2 || ragStop[f] {
			continue
		}
		out = append(out, f)
	}
	return out
}

func scoreOverlap(query []string, text string) int {
	if len(query) == 0 {
		return 0
	}
	have := map[string]bool{}
	for _, t := range tokenize(text) {
		have[t] = true
	}
	n := 0
	for _, q := range query {
		if have[q] {
			n++
		}
	}
	return n
}

// chunkDigest splits a digest into ## sections (fallback: non-empty paragraphs).
func chunkDigest(digest string) []string {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil
	}
	parts := strings.Split(digest, "\n## ")
	var chunks []string
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if i > 0 {
			p = "## " + p
		}
		chunks = append(chunks, p)
	}
	if len(chunks) <= 1 {
		var paras []string
		for _, para := range strings.Split(digest, "\n\n") {
			para = strings.TrimSpace(para)
			if para != "" {
				paras = append(paras, para)
			}
		}
		return paras
	}
	return chunks
}

func renderArchiveContext(chunks []scoredChunk, maxChars int) string {
	if len(chunks) == 0 || maxChars <= 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Prior digest context (our archive; not corroborated):\n")
	budget := maxChars
	for _, c := range chunks {
		block := "- " + strings.ReplaceAll(strings.TrimSpace(c.Text), "\n", "\n  ") + "\n"
		if len(block) > budget {
			const ellipsis = "…\n"
			if budget <= len(ellipsis) {
				break
			}
			block = block[:budget-len(ellipsis)] + ellipsis
			b.WriteString(block)
			break
		}
		b.WriteString(block)
		budget -= len(block)
	}
	return b.String()
}

// retrieveDigestContext loads English digests since `since`, ranks sections by lexical overlap with query,
// returns a prompt block (empty if nothing useful).
func retrieveDigestContext(ctx context.Context, tools agent.ToolSource, since time.Time, query string, k, maxChars int) (string, error) {
	qtoks := tokenize(query)
	if len(qtoks) == 0 || k <= 0 {
		return "", nil
	}
	args, err := json.Marshal(map[string]string{"since": since.Format("2006-01-02")})
	if err != nil {
		return "", err
	}
	out, isErr, err := tools.Call(ctx, "list_runs", args)
	if err != nil {
		return "", fmt.Errorf("list_runs: %w", err)
	}
	if isErr {
		return "", fmt.Errorf("list_runs: %s", out)
	}
	var runs struct {
		Keys []string `json:"keys"`
	}
	if err := json.Unmarshal([]byte(out), &runs); err != nil {
		return "", fmt.Errorf("parse list_runs: %w", err)
	}
	var scored []scoredChunk
	for _, key := range runs.Keys {
		args, err := json.Marshal(map[string]any{"key": key, "includeTweets": false})
		if err != nil {
			return "", err
		}
		out, isErr, err := tools.Call(ctx, "get_artifact", args)
		if err != nil {
			return "", fmt.Errorf("get_artifact %s: %w", key, err)
		}
		if isErr {
			return "", fmt.Errorf("get_artifact %s: %s", key, out)
		}
		var got struct {
			Artifact struct {
				Language string `json:"language"`
				Digest   string `json:"digest"`
			} `json:"artifact"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			return "", fmt.Errorf("parse artifact %s: %w", key, err)
		}
		if !strings.EqualFold(got.Artifact.Language, "English") {
			continue
		}
		for _, chunk := range chunkDigest(got.Artifact.Digest) {
			s := scoreOverlap(qtoks, chunk)
			if s == 0 {
				continue
			}
			scored = append(scored, scoredChunk{Text: chunk, Score: s})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	if len(scored) > k {
		scored = scored[:k]
	}
	return renderArchiveContext(scored, maxChars), nil
}
