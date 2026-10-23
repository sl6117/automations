package twitterdigest

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/pkg/sources"
)

// jevScaleDefect marks rollup values as P(defect). Older artifacts omit the
// field and stored P(pass) on the same four dimension names.
const jevScaleDefect = "defect"

const (
	jevSendAt   = 0.2
	jevRepairAt = 0.8
)

// jevScore is one Jev call. Detail is P(yes) on each defect question.
// Rollup is the worst defect in each dimension, which is what a threshold would read.
type jevScore struct {
	Rollup map[string]float64
	Detail map[string]float64
}

type digestBullet struct {
	Section string
	Text    string
}

// digestBullets returns each "- " line with the ## section it sits under.
func digestBullets(digest string) []digestBullet {
	var out []digestBullet
	section := ""
	for _, line := range strings.Split(digest, "\n") {
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		trimmed := strings.TrimSpace(line)
		if section != "" && strings.HasPrefix(trimmed, "- ") {
			out = append(out, digestBullet{Section: section, Text: strings.TrimPrefix(trimmed, "- ")})
		}
	}
	return out
}

// jevQuestions asks one defect per bullet, one for a story split across bullets,
// and one per source tweet for a concrete event the digest never mentions.
// A high answer means the defect is present.
func jevQuestions(topics []Topic, kept []sources.Tweet, digest string) map[string]string {
	q := map[string]string{}
	for i, b := range digestBullets(digest) {
		id := fmt.Sprintf("b%d", i)
		quoted := fmt.Sprintf("Bullet under %q: %s", b.Section, b.Text)
		q[id+"_fact"] = quoted + "\nDoes this bullet state a number, name, date, or place that its cited tweet does not?"
		q[id+"_cite"] = quoted + "\nDoes this bullet cite a tweet that does not contain the claim?"
		q[id+"_attr"] = quoted + "\nDoes this bullet state, as the digest's own fact, something its cited tweet only attributes to someone else?"
		q[id+"_section"] = "Allowed topics:\n" + topicList(topics) + "\n" + quoted + "\nIs this bullet in a section the topic list rules out? A defensible section, including Other, is a no."
		q[id+"_merged"] = quoted + "\nDoes this bullet join two unrelated events?"
	}
	q["split"] = "Does the digest tell the same event in more than one bullet?"
	for i, t := range kept {
		q[fmt.Sprintf("t%d_missing", i)] = fmt.Sprintf("Tweet by %s: %s\nDoes this tweet state a concrete event that no digest bullet mentions? A duplicate, a meme, or a bare link is a no.", t.Handle, t.Text)
	}
	return q
}

func jevState(topics []Topic, kept []sources.Tweet, digest, language string) (string, error) {
	tweetsJSON, err := slimTweets(kept)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Language: %s\n\nAllowed topics:\n%s\n\nSource tweets:\n%s\n\nDigest:\n%s",
		language, topicList(topics), tweetsJSON, digest), nil
}

// jevRollup reduces the defect questions to the four dimensions. Each value is
// the highest P(defect) among the questions for that dimension.
func jevRollup(detail map[string]float64) map[string]float64 {
	maxOf := func(match func(string) bool) float64 {
		var worst float64
		for name, score := range detail {
			if match(name) && score > worst {
				worst = score
			}
		}
		return worst
	}
	return map[string]float64{
		"faithfulness": maxOf(func(name string) bool {
			return strings.HasSuffix(name, "_fact") || strings.HasSuffix(name, "_cite") || strings.HasSuffix(name, "_attr")
		}),
		"topicRouting": maxOf(func(name string) bool { return strings.HasSuffix(name, "_section") }),
		"coverage":     maxOf(func(name string) bool { return strings.HasSuffix(name, "_missing") }),
		"clarity":      maxOf(func(name string) bool { return name == "split" || strings.HasSuffix(name, "_merged") }),
	}
}

// jevShadow asks Jev for P(defect) on each bullet and tweet. The caller logs a failure and still delivers.
func jevShadow(ctx context.Context, client ai.Jev, topics []Topic, kept []sources.Tweet, digest, language string) (jevScore, error) {
	state, err := jevState(topics, kept, digest, language)
	if err != nil {
		return jevScore{}, err
	}
	detail, err := client.Nouls(ctx, state, jevQuestions(topics, kept, digest))
	if err != nil {
		return jevScore{}, err
	}
	return jevScore{Rollup: jevRollup(detail), Detail: detail}, nil
}

// jevRoute is the cascade decision for one draft score. An empty string means Jev did not score.
func jevRoute(score jevScore, callErr string) string {
	if callErr != "" {
		return "escalate"
	}
	if len(score.Detail) == 0 {
		return ""
	}
	var ids []string
	for name, p := range score.Detail {
		if p >= jevRepairAt && jevRepairable(name) {
			ids = append(ids, name)
		}
	}
	if len(ids) > 0 {
		sort.Strings(ids)
		return "repair:" + strings.Join(ids, ",")
	}
	for _, name := range []string{"faithfulness", "topicRouting", "coverage", "clarity"} {
		if score.Rollup[name] > jevSendAt {
			return "escalate"
		}
	}
	return "send"
}
func jevRepairable(name string) bool {
	switch {
	case name == "split":
		return true
	case strings.HasSuffix(name, "_fact"), strings.HasSuffix(name, "_cite"), strings.HasSuffix(name, "_attr"),
		strings.HasSuffix(name, "_merged"), strings.HasSuffix(name, "_section"), strings.HasSuffix(name, "_missing"):
		return true
	default:
		return false
	}
}
