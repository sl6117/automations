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

const magnitudeTolerance = 0.10

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
			matched := false
			for _, c := range cited {
				if closeMagnitude(q, c) {
					matched = true
					break
				}
			}
			if !matched {
				failures = append(failures, fmt.Sprintf("magnitude %.4g not in cited tweets", q))
			}
		}
	}
	return failures
}
