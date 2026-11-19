package twitterdigest

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sl6117/automations/pkg/sources"
)

// xURLPattern matches x.com status links in rendered digest text
// the character class stops at whitespace and punctuation the model may wrap a link in ")" or similar
// Safe bc X usernames and status ids never contain those characters
var xURLPattern = regexp.MustCompile(`https://x\.com/[^\s\])",.;]+`)
var (
	numberToken     = regexp.MustCompile(`\d+(?:\.\d+)?`)
	spacedThousands = regexp.MustCompile(`(\d{1,3})[ \x{00A0}\x{202F}](\d{3})(\D|$)`)
	thousandsComma  = regexp.MustCompile(`(\d),(\d{3})(\D|$)`)
	decimalComma    = regexp.MustCompile(`(\d),(\d{1,2})(\D|$)`)
	unitScale       = map[string]float64{
		"천": 1e3, "만": 1e4, "억": 1e8, "조": 1e12,
		"thousand": 1e3, "million": 1e6, "billion": 1e9, "trillion": 1e12,
		"тыс": 1e3, "млн": 1e6, "млрд": 1e9, "трлн": 1e12,
		"k": 1e3, "m": 1e6, "b": 1e9, "t": 1e12,
	}
)

const (
	magnitudeTolerance = 0.10
	// smallCountLimit is the floor for a bare number that is not in the cited
	// tweet. A count of 2 is not a failure. A ten-times miss of a large cited
	// figure still is, even when the digest side of that pair is small.
	smallCountLimit = 100
	// scaleMissFloor is the larger of the digest figure and the cited figure.
	// Below this, a 10x pair is still a small count and does not hold a bullet.
	scaleMissFloor = 1000
)

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
	failures = append(failures, unsupportedMagnitudes(digest, kept)...)
	return failures, coverage
}

func normalizeNumerals(text string) string {
	text = xURLPattern.ReplaceAllString(text, " ")
	for _, pattern := range []*regexp.Regexp{spacedThousands, thousandsComma} {
		for {
			next := pattern.ReplaceAllString(text, "${1}${2}${3}")
			if next == text {
				break
			}
			text = next
		}
	}
	return decimalComma.ReplaceAllString(text, "${1}.${2}${3}")
}

func scaleOf(rest string, attached bool) float64 {
	rest = strings.TrimLeft(rest, " \t")
	if rest == "" || strings.HasPrefix(rest, "%") {
		return 1
	}
	i := 0
	for i < len(rest) {
		r, size := utf8.DecodeRuneInString(rest[i:])
		if !unicode.IsLetter(r) {
			break
		}
		i += size
	}
	if i == 0 {
		return 1
	}
	word := strings.TrimRight(strings.ToLower(rest[:i]), ".")
	word = strings.TrimSuffix(word, "원")
	word = strings.TrimSuffix(word, "달러")
	if (word == "k" || word == "m" || word == "b" || word == "t") && !attached {
		return 1
	}
	if s, ok := unitScale[word]; ok {
		return s
	}
	return 1
}

func quantities(text string) []float64 {
	text = normalizeNumerals(text)
	var out []float64
	for _, loc := range numberToken.FindAllStringIndex(text, -1) {
		if embeddedNumber(text, loc[0]) || digitsOf(text[loc[0]:loc[1]]) > 12 {
			continue
		}
		v, err := strconv.ParseFloat(text[loc[0]:loc[1]], 64)
		if err != nil {
			continue
		}
		attached := loc[1] < len(text) && text[loc[1]] != ' ' && text[loc[1]] != '\t'
		out = append(out, v*scaleOf(text[loc[1]:], attached))
	}
	return out
}

// embeddedNumber reports a digit run glued to the end of a word, as in Breaking911.
func embeddedNumber(text string, start int) bool {
	if start == 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(text[:start])
	return unicode.IsLetter(r)
}
func digitsOf(raw string) int {
	n := 0
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			n++
		}
	}
	return n
}

func closeMagnitude(a, b float64) bool {
	base := math.Max(math.Abs(a), math.Abs(b))
	if base == 0 {
		return true
	}
	return math.Abs(a-b)/base <= magnitudeTolerance
}

func unsupportedMagnitudes(digest string, kept []sources.Tweet) []string {
	qtyByURL := make(map[string][]float64, len(kept))
	for _, t := range kept {
		qtyByURL[t.URL] = quantities(t.Text)
	}
	var failures []string
	for _, line := range strings.Split(digest, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		var cited []float64
		keptCite := false
		for _, u := range xURLPattern.FindAllString(trimmed, -1) {
			qs, ok := qtyByURL[u]
			if !ok {
				continue
			}
			keptCite = true
			cited = append(cited, qs...)
		}
		if !keptCite {
			continue
		}
		seen := map[string]bool{}
		for _, q := range quantities(trimmed) {
			key := strconv.FormatFloat(q, 'g', -1, 64)
			if seen[key] {
				continue
			}
			seen[key] = true
			if _, scale := quantityIssue(q, cited); scale || unmatchedMagnitude(q, cited) {
				failures = append(failures, fmt.Sprintf("magnitude %.4g not in cited tweets", q))
			}
		}
	}
	return failures
}

// quantityIssue reports a 10x, 100x, or 1000x gap against a cited figure
// when the larger of the two is at least scaleMissFloor.
// A matching figure returns 0. A small count that is merely absent returns 0.
func quantityIssue(q float64, cited []float64) (float64, bool) {
	for _, c := range cited {
		if closeMagnitude(q, c) {
			return 0, false
		}
	}
	for _, c := range cited {
		if c == 0 || q == 0 {
			continue
		}
		if math.Max(math.Abs(q), math.Abs(c)) < scaleMissFloor {
			continue
		}
		ratio := math.Abs(q / c)
		if ratio < 1 {
			ratio = 1 / ratio
		}
		for _, factor := range []float64{10, 100, 1000} {
			if closeMagnitude(ratio, factor) {
				return factor, true
			}
		}
	}
	return 0, false
}

func unmatchedMagnitude(q float64, cited []float64) bool {
	if _, scale := quantityIssue(q, cited); scale {
		return false
	}
	for _, c := range cited {
		if closeMagnitude(q, c) {
			return false
		}
	}
	return math.Abs(q) >= smallCountLimit
}

// holdScaleMisses drops bullets whose number is about 10, 100, or 1000 times
// a figure in a cited tweet. A count of 2 does not drop a bullet.
// Empty sections are dropped with their last bullet. The returned notes name
// what was held.
func holdScaleMisses(digest string, kept []sources.Tweet) (string, []string) {
	qtyByURL := make(map[string][]float64, len(kept))
	for _, t := range kept {
		qtyByURL[t.URL] = quantities(t.Text)
	}
	type section struct {
		header  string
		body    []string
		bullets int
	}
	var preamble []string
	var sections []section
	var notes []string
	idx := -1
	for _, line := range strings.Split(digest, "\n") {
		if strings.HasPrefix(line, "## ") {
			sections = append(sections, section{header: line})
			idx = len(sections) - 1
			continue
		}
		if idx < 0 {
			preamble = append(preamble, line)
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			if q, factor, ok := bulletScaleMiss(trimmed, qtyByURL); ok {
				notes = append(notes, fmt.Sprintf("held bullet: magnitude %.4g is %.0fx a cited figure: %s", q, factor, clipLine(trimmed, 160)))
				continue
			}
			sections[idx].bullets++
		}
		sections[idx].body = append(sections[idx].body, line)
	}
	var b strings.Builder
	for _, line := range preamble {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, s := range sections {
		if s.bullets == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(s.header)
		b.WriteByte('\n')
		body := s.body
		for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
			body = body[:len(body)-1]
		}
		for _, line := range body {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n"), notes
}

func bulletScaleMiss(line string, qtyByURL map[string][]float64) (float64, float64, bool) {
	var cited []float64
	keptCite := false
	for _, u := range xURLPattern.FindAllString(line, -1) {
		qs, ok := qtyByURL[u]
		if !ok {
			continue
		}
		keptCite = true
		cited = append(cited, qs...)
	}
	if !keptCite {
		return 0, 0, false
	}
	seen := map[string]bool{}
	for _, q := range quantities(line) {
		key := strconv.FormatFloat(q, 'g', -1, 64)
		if seen[key] {
			continue
		}
		seen[key] = true
		if factor, ok := quantityIssue(q, cited); ok {
			return q, factor, true
		}
	}
	return 0, 0, false
}

func clipLine(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
