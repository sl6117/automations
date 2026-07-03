package twitterdigest

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/sl6117/automations/pkg/sources"
)

// xURLPattern matches x.com satus links in rendered digest text
// the character class stops at whitespace and punctuation the model may wrap a link in ")" or similar
// Safe bc X usernames and status ids never contain those characters
var xURLPattern = regexp.MustCompile(`https://x\.com/[^\s\])",.]+`)

func evalDigest(digest string, kept []sources.Tweet, topics []Topic) (failures []string, coverage string) {
	keptURLs := make(map[string]bool, len(kept))

	for _, t := range kept {
		keptURLs[t.URL] = true
	}

	citedCount := make(map[string]int)

	for _, u := range xURLPattern.FindAllString(digest, -1) {
		citedCount[u]++
	}

	for u, n := range citedCount {
		if !keptURLs[u] {
			failures = append(failures, "hallucinated url: "+u)
		}
		if n > 1 {
			failures = append(failures, fmt.Sprintf("url cited %d times (duplicate story?): %s", n, u))
		}
	}

	allowed := map[string]bool{"Other": true}

	for _, t := range topics {
		allowed[t.Name] = true
	}
	for _, line := range strings.Split(digest, "\n") {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, "## "))
		if !allowed[name] {
			failures = append(failures, fmt.Sprintf("unknown section: %q", name))
		}
	}

	citedKept := 0
	for u := range citedCount {
		if keptURLs[u] {
			citedKept++
		}
	}
	coverage = fmt.Sprintf("%d/%d kept tweets cited", citedKept, len(kept))
	return failures, coverage
}
