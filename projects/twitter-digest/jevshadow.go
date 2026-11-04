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

// jevQuestions asks one defect per bullet, one per bullet for the same event
// told again, and one per source tweet for a news event the digest never mentions.
// A high answer means the defect is present.
func jevQuestions(topics []Topic, kept []sources.Tweet, digest string) map[string]ai.Noul {
	q := map[string]ai.Noul{}
	ask := func(instructions string) ai.Noul {
		return ai.Noul{Instructions: instructions}
	}
	for i, b := range digestBullets(digest) {
		id := fmt.Sprintf("b%d", i)
		quoted := fmt.Sprintf("Bullet under %q: %s", b.Section, b.Text)
		q[id+"_fact"] = ask(quoted + "\nDoes this bullet state a number, name, date, or place that its cited tweet does not?")
		q[id+"_cite"] = ask(quoted + "\nDoes this bullet cite a tweet that does not contain the claim?")
		q[id+"_attr"] = ai.Noul{
			Instructions: quoted + "\nDoes this bullet drop who said the claim, so a reader would take it as the digest's own observation?",
			Criteria: &ai.NoulCriteria{
				True:  `The bullet states the claim with no speaker and no "according to the source."`,
				False: "The bullet names the speaker, or says the claim is according to the source.",
			},
		}
		q[id+"_section"] = ask("Allowed topics:\n" + topicList(topics) + "\n" + quoted + "\nIs this bullet in a section the topic list rules out? A defensible section, including Other, is a no.")
		q[id+"_merged"] = ask(quoted + "\nDoes this bullet join two unrelated events?")
		q[id+"_again"] = ask(quoted + "\nDoes another bullet tell this same event again? A later update, a reaction, or a separate fact from the same place is a no.")
		q[id+"_hedge"] = ask(quoted + "\nDoes this bullet state as finished something its cited tweet only says is on track, expected, or possible?")
	}
	for i, t := range kept {
		q[fmt.Sprintf("t%d_missing", i)] = ask(fmt.Sprintf("Tweet by %s: %s\nDoes this tweet report a news event that no digest bullet mentions? A photo or video caption, a meme, a bare link, a personal note, a nomination list, a promotional post, a market-analysis thread, or a duplicate of a story already covered is a no.", t.Handle, t.Text))
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
			return strings.HasSuffix(name, "_fact") || strings.HasSuffix(name, "_cite")
		}),
		"topicRouting": maxOf(func(name string) bool { return strings.HasSuffix(name, "_section") }),
		"coverage":     maxOf(func(name string) bool { return strings.HasSuffix(name, "_missing") }),
		"clarity":      maxOf(func(name string) bool { return strings.HasSuffix(name, "_merged") }),
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

// jevRepairable names the questions that can set a repair: route.
// _again, _attr, and _hedge stay in the detail and do not route.
func jevRepairable(name string) bool {
	switch {
	case strings.HasSuffix(name, "_fact"), strings.HasSuffix(name, "_cite"),
		strings.HasSuffix(name, "_merged"), strings.HasSuffix(name, "_section"), strings.HasSuffix(name, "_missing"):
		return true
	default:
		return false
	}
}

// jevFaithIDs lists fact and cite questions at or above the repair line, sorted.
func jevFaithIDs(detail map[string]float64) []string {
	var ids []string
	for name, p := range detail {
		if p >= jevRepairAt && (strings.HasSuffix(name, "_fact") || strings.HasSuffix(name, "_cite")) {
			ids = append(ids, name)
		}
	}
	sort.Strings(ids)
	return ids
}
func bulletIndex(id string) int {
	if !strings.HasPrefix(id, "b") {
		return -1
	}
	n := 0
	digits := 0
	for _, r := range id[1:] {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
		digits++
	}
	if digits == 0 {
		return -1
	}
	return n
}

// jevFaithCritique names the bullets a fact or cite score says to rewrite.
// An empty string means this draft has nothing for the faithfulness repair.
func jevFaithCritique(digest string, detail map[string]float64) string {
	ids := jevFaithIDs(detail)
	if len(ids) == 0 {
		return ""
	}
	bullets := digestBullets(digest)
	fact := map[int]bool{}
	cite := map[int]bool{}
	var order []int
	seen := map[int]bool{}
	for _, id := range ids {
		n := bulletIndex(id)
		if n < 0 || n >= len(bullets) {
			continue
		}
		if !seen[n] {
			seen[n] = true
			order = append(order, n)
		}
		if strings.HasSuffix(id, "_fact") {
			fact[n] = true
		}
		if strings.HasSuffix(id, "_cite") {
			cite[n] = true
		}
	}
	sort.Ints(order)
	if len(order) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Rewrite only the bullets named here. Leave every other bullet unchanged.\n")
	for _, n := range order {
		reason := "states a number, name, date, or place its cited tweet does not"
		switch {
		case fact[n] && cite[n]:
			reason = "states a number, name, date, or place its cited tweet does not, and cites a tweet that does not contain the claim"
		case cite[n]:
			reason = "cites a tweet that does not contain the claim"
		}
		b.WriteString(fmt.Sprintf("b%d (%s): %s\n", n, reason, bullets[n].Text))
	}
	return b.String()
}

// tweetsForFaithRepair keeps the tweets cited by a fact or cite repair.
// With no such repair, or no status id that matches a kept tweet, the caller still sees
// every kept tweet.
func tweetsForFaithRepair(digest string, detail map[string]float64, kept []sources.Tweet) []sources.Tweet {
	ids := jevFaithIDs(detail)
	if len(ids) == 0 {
		return kept
	}
	bullets := digestBullets(digest)
	want := map[string]bool{}
	for _, id := range ids {
		n := bulletIndex(id)
		if n < 0 || n >= len(bullets) {
			continue
		}
		for _, m := range citationID.FindAllStringSubmatch(bullets[n].Text, -1) {
			want[m[1]] = true
		}
	}
	if len(want) == 0 {
		return kept
	}
	var out []sources.Tweet
	for _, t := range kept {
		if want[t.ID] {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return kept
	}
	return out
}
