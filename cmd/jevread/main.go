package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sl6117/automations/internal/storage"
	twitterdigest "github.com/sl6117/automations/projects/twitter-digest"
)

func main() {
	ctx := context.Background()
	store, err := storage.FromEnv(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "storage: %v\n", err)
		os.Exit(1)
	}
	keys, err := store.List(ctx, "logs/runs/")
	if err != nil {
		fmt.Fprintf(os.Stderr, "list: %v\n", err)
		os.Exit(1)
	}

	type famStat struct {
		n, low, mid, high int
		max               float64
	}
	eras := map[string]map[string]*famStat{}
	bump := func(era, fam string, p float64) {
		if eras[era] == nil {
			eras[era] = map[string]*famStat{}
		}
		s := eras[era][fam]
		if s == nil {
			s = &famStat{}
			eras[era][fam] = s
		}
		s.n++
		switch {
		case p <= 0.2:
			s.low++
		case p >= 0.8:
			s.high++
		default:
			s.mid++
		}
		if p > s.max {
			s.max = p
		}
	}

	for _, key := range keys {
		if !strings.Contains(key, "twitter-digest") {
			continue
		}
		date := ""
		if i := strings.Index(key, "2026-11-"); i >= 0 {
			date = key[i : i+10]
		}
		if date < "2026-11-04" || date > "2026-11-16" {
			continue
		}
		era := "prev-image"
		if date >= "2026-11-10" {
			era = "new-image"
		} else if date == "2026-11-04" {
			era = "before-attr"
		}

		raw, err := store.Get(ctx, key)
		if err != nil {
			fmt.Printf("ERR get %s: %v\n", key, err)
			continue
		}
		var a twitterdigest.Artifact
		if err := json.Unmarshal(raw, &a); err != nil {
			fmt.Printf("ERR json %s: %v\n", key, err)
			continue
		}

		fmt.Printf("\n===== %s %s scale=%q route=%q adopted=%v detail=%d err=%q jevErr=%q =====\n",
			date, a.Language, a.JevScale, a.JevRoute, a.RevisionAdopted, len(a.InitialJevDetail), a.InitialJevError, a.JevError)
		fmt.Printf("headers: %s\n", strings.Join(headers(a.Digest), " | "))
		if era == "new-image" && len(a.EvalFailures) > 0 {
			fmt.Printf("evalFailures: %s\n", clip(strings.Join(a.EvalFailures, " || "), 500))
		}
		fmt.Printf("rollup initial=%s sent=%s\n", fmtMap(a.InitialJev), fmtMap(a.Jev))
		if a.InitialJudge != nil {
			fmt.Printf("sonnet-draft faith=%v cover=%v route=%v clarity=%v\n",
				a.InitialJudge.Faithfulness.Pass, a.InitialJudge.Coverage.Pass, a.InitialJudge.TopicRouting.Pass, a.InitialJudge.Clarity.Pass)
			fmt.Printf("  cover-why: %s\n", clip(a.InitialJudge.Coverage.Reason, 420))
			if !a.InitialJudge.Faithfulness.Pass {
				fmt.Printf("  faith-why: %s\n", clip(a.InitialJudge.Faithfulness.Reason, 360))
			}
			if !a.InitialJudge.Clarity.Pass {
				fmt.Printf("  clarity-why: %s\n", clip(a.InitialJudge.Clarity.Reason, 280))
			}
			if !a.InitialJudge.TopicRouting.Pass {
				fmt.Printf("  route-why: %s\n", clip(a.InitialJudge.TopicRouting.Reason, 280))
			}
		} else {
			fmt.Printf("sonnet-draft: none err=%q\n", a.JudgeError)
		}

		found, body := sectionBody(a.Digest, "Celebrity")
		if found {
			fmt.Printf("CELEBRITY:\n%s\n", clip(body, 1200))
		} else if era == "new-image" {
			fmt.Printf("NO_CELEBRITY\n")
		}
		if era == "new-image" {
			_, other := sectionBody(a.Digest, "Other")
			if strings.TrimSpace(other) != "" {
				fmt.Printf("OTHER:\n%s\n", clip(other, 900))
			}
		}

		bullets := digestBullets(a.Digest)
		var notable []string
		ids := make([]string, 0, len(a.InitialJevDetail))
		for id := range a.InitialJevDetail {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			p := a.InitialJevDetail[id]
			fam := family(id)
			bump(era, fam, p)
			show := p >= 0.8
			if p >= 0.55 && (fam == "hedge" || fam == "attr" || fam == "again" || fam == "section" || fam == "missing" || fam == "fact" || fam == "cite" || fam == "merged") {
				show = p >= 0.55
			}
			if !show {
				continue
			}
			text := contextFor(id, bullets, a)
			notable = append(notable, fmt.Sprintf("  %s %.2f %s", id, p, clip(oneLine(text), 320)))
		}
		if len(notable) > 0 {
			fmt.Printf("scores>=0.55:\n%s\n", strings.Join(notable, "\n"))
		}

		if era == "new-image" {
			for i, t := range a.Kept {
				id := fmt.Sprintf("t%d_missing", i)
				p, ok := a.InitialJevDetail[id]
				if !ok {
					continue
				}
				blob := strings.ToLower(t.Handle + " " + t.Text)
				if keywordHit(blob) {
					fmt.Printf("  keyword-missing %s %.2f @%s %s\n", id, p, t.Handle, clip(oneLine(t.Text), 240))
				}
			}
		}
	}

	fmt.Printf("\n\n===== BANDS (low<=0.2, high>=0.8) =====\n")
	eraOrder := []string{"before-attr", "prev-image", "new-image"}
	famOrder := []string{"fact", "cite", "attr", "section", "merged", "again", "hedge", "missing"}
	for _, era := range eraOrder {
		fmt.Printf("\n%s\n", era)
		for _, fam := range famOrder {
			s := eras[era][fam]
			if s == nil {
				fmt.Printf("  %-8s none\n", fam)
				continue
			}
			fmt.Printf("  %-8s n=%3d low=%3d mid=%3d high=%2d max=%.2f\n", fam, s.n, s.low, s.mid, s.high, s.max)
		}
	}
}

func family(id string) string {
	switch {
	case strings.HasSuffix(id, "_fact"):
		return "fact"
	case strings.HasSuffix(id, "_cite"):
		return "cite"
	case strings.HasSuffix(id, "_attr"):
		return "attr"
	case strings.HasSuffix(id, "_section"):
		return "section"
	case strings.HasSuffix(id, "_merged"):
		return "merged"
	case strings.HasSuffix(id, "_again"):
		return "again"
	case strings.HasSuffix(id, "_hedge"):
		return "hedge"
	case strings.HasSuffix(id, "_missing"):
		return "missing"
	default:
		return "other"
	}
}

func keywordHit(blob string) bool {
	for _, w := range []string{"polymarket", "odds", "predict", "prayer", "rosary", "procession", "quran", "giveaway", "stunt", "meme", "dairy queen", "free "} {
		if strings.Contains(blob, w) {
			return true
		}
	}
	return false
}

func headers(digest string) []string {
	var out []string
	for _, line := range strings.Split(digest, "\n") {
		if strings.HasPrefix(line, "## ") {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(line, "## ")))
		}
	}
	return out
}

func sectionBody(digest, name string) (bool, string) {
	var b strings.Builder
	in := false
	found := false
	for _, line := range strings.Split(digest, "\n") {
		if strings.HasPrefix(line, "## ") {
			if in {
				break
			}
			title := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if strings.EqualFold(title, name) {
				in = true
				found = true
			}
			continue
		}
		if in {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return found, strings.TrimSpace(b.String())
}

type bullet struct {
	Section string
	Text    string
}

func digestBullets(digest string) []bullet {
	var out []bullet
	section := ""
	for _, line := range strings.Split(digest, "\n") {
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		trimmed := strings.TrimSpace(line)
		if section != "" && strings.HasPrefix(trimmed, "- ") {
			out = append(out, bullet{Section: section, Text: strings.TrimPrefix(trimmed, "- ")})
		}
	}
	return out
}

func contextFor(id string, bullets []bullet, a twitterdigest.Artifact) string {
	if strings.HasPrefix(id, "b") {
		n := indexOf(id)
		if n >= 0 && n < len(bullets) {
			return "[" + bullets[n].Section + "] " + bullets[n].Text
		}
		return "(bullet missing)"
	}
	if strings.Contains(id, "_missing") {
		n := indexOf(id)
		if n >= 0 && n < len(a.Kept) {
			t := a.Kept[n]
			return "@" + t.Handle + " " + t.Text
		}
		return "(tweet missing)"
	}
	return ""
}

func indexOf(id string) int {
	i := 0
	for i < len(id) && (id[i] < '0' || id[i] > '9') {
		i++
	}
	n := 0
	digits := 0
	for i < len(id) && id[i] >= '0' && id[i] <= '9' {
		n = n*10 + int(id[i]-'0')
		digits++
		i++
	}
	if digits == 0 {
		return -1
	}
	return n
}

func fmtMap(m map[string]float64) string {
	if len(m) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%.2f", k, m[k]))
	}
	return strings.Join(parts, " ")
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
