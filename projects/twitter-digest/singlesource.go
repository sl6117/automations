package twitterdigest

import (
	"regexp"
	"strings"

	"github.com/sl6117/automations/pkg/sources"
)

// singleSourceLabel marks a bullet whose story rests on one distinct voice.
// Appended at delivery only: eval, judge, revise, and the stored artifact all see the model's unlabeled text.
const singleSourceLabel = " [single-source]"

// rtPattern captures the original author of a retweet. The pipeline marks retweets as "RT @user: ..." and the retweeter amplifies,
// it doesn't source: 2 accounts retweeting the same post are one voice, not two.
var rtPattern = regexp.MustCompile(`^RT @(\w+):`)

// annotateSingleSource appends singleSourceLabel to each digest bullet whose citations trace back to exactly one distinct voice.
// Counting is by status id (via citationID) mapped to kept posts, never by parsing handles out of the bullet:
// the model mangles handles but cannot mangle ids. A bullet with no resolvable citation is left alone - unknown is not single-source.
// Annotate only: never suppress, reorder, or touch non-bullet lines.
func annotateSingleSource(digest string, kept []sources.Tweet) string {
	sourceOf := make(map[string]string, len(kept)) // status id -> attributed voice
	for _, t := range kept {
		handle := t.Handle
		if m := rtPattern.FindStringSubmatch(t.Text); m != nil {
			handle = m[1]
		}
		sourceOf[t.ID] = strings.ToLower(handle)
	}

	lines := strings.Split(digest, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		voices := map[string]bool{}
		for _, m := range citationID.FindAllStringSubmatch(line, -1) {
			if v, ok := sourceOf[m[1]]; ok {
				voices[v] = true
			}
		}
		if len(voices) == 1 {
			lines[i] = line + singleSourceLabel
		}
	}
	return strings.Join(lines, "\n")
}
