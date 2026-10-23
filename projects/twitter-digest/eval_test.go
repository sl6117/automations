package twitterdigest

import (
	"strings"
	"testing"

	"github.com/sl6117/automations/pkg/sources"
)

func TestEvalDigest(t *testing.T) {
	kept := []sources.Tweet{
		{URL: "https://x.com/a/status/1"},
		{URL: "https://x.com/b/status/2"},
	}
	topics := []Topic{{Name: "AI"}}

	t.Run("clean digest passes", func(t *testing.T) {
		digest := "## AI\n- point one (https://x.com/a/status/1)\n- point two (https://x.com/b/status/2)"
		failures, coverage := evalDigest(digest, kept, topics)

		if len(failures) != 0 {
			t.Errorf("failures = %v, want none", failures)
		}
		if coverage != "2/2 kept tweets cited" {
			t.Errorf("coverage = %q, want 2/2 kept tweets cited", coverage)
		}
	})

	t.Run("catches hallucinated url", func(t *testing.T) {
		digest := "## AI\n- made up (https://x.com/a/status/999)"
		failures, _ := evalDigest(digest, kept, topics)
		if len(failures) != 1 || !strings.Contains(failures[0], "hallucinated") {
			t.Errorf("failures = %v, want one hallucinated-url failure", failures)
		}
	})

	t.Run("catches duplicate citation", func(t *testing.T) {
		digest := "## AI\n- one (https://x.com/a/status/1)\n- again (https://x.com/a/status/1)"
		failures, _ := evalDigest(digest, kept, topics)
		if len(failures) != 1 || !strings.Contains(failures[0], "cited 2 times") {
			t.Errorf("failures = %v, want one duplicate failure", failures)
		}
	})

	t.Run("catches invented section", func(t *testing.T) {
		digest := "## Sports\n- huh (https://x.com/a/status/1)"
		failures, _ := evalDigest(digest, kept, topics)
		if len(failures) != 1 || !strings.Contains(failures[0], "unknown section") {
			t.Errorf("failures = %v, want one unknown-section failure", failures)
		}
	})

	t.Run("trailing punctuation does not corrupt urls", func(t *testing.T) {
		digest := "## AI\n- end of sentence https://x.com/a/status/1."
		failures, coverage := evalDigest(digest, kept, topics)
		if len(failures) != 0 {
			t.Errorf("failures = %v, want none (period should not join the url)", failures)
		}
		if !strings.HasPrefix(coverage, "1/2") {
			t.Errorf("coverage = %q, want 1/2", coverage)
		}
	})
	t.Run("semicolon-merged citations parse as separate urls", func(t *testing.T) {
		digest := "## AI\n- merged story (@a, https://x.com/a/status/1; @b, https://x.com/b/status/2)"
		failures, coverage := evalDigest(digest, kept, topics)
		if len(failures) != 0 {
			t.Errorf("failures = %v, want none (semicolon separates citations)", failures)
		}
		if !strings.HasPrefix(coverage, "2/2") {
			t.Errorf("coverage = %q, want 2/2", coverage)
		}
	})

}
func TestEvalMagnitudes(t *testing.T) {
	kept := []sources.Tweet{
		{URL: "https://x.com/a/status/1", Text: "매출 1.5억원"},
		{URL: "https://x.com/b/status/2", Text: "Revenue grew 20%"},
		{URL: "https://x.com/c/status/3", Text: "Headcount is 1500"},
	}
	topics := []Topic{{Name: "AI"}}

	t.Run("억 matches million", func(t *testing.T) {
		digest := "## AI\n- Revenue was $150 million (@a https://x.com/a/status/1)"
		failures, _ := evalDigest(digest, kept, topics)
		if len(failures) != 0 {
			t.Errorf("failures = %v, want none", failures)
		}
	})

	t.Run("억 does not match billion", func(t *testing.T) {
		digest := "## AI\n- Revenue was $1.5 billion (@a https://x.com/a/status/1)"
		failures, _ := evalDigest(digest, kept, topics)
		if len(failures) != 1 || !strings.Contains(failures[0], "1.5e+09") {
			t.Errorf("failures = %v, want the 1.5 billion count", failures)
		}
	})

	t.Run("percents stay raw", func(t *testing.T) {
		digest := "## AI\n- 매출이 20% 늘었다 (@b https://x.com/b/status/2)"
		failures, _ := evalDigest(digest, kept, topics)
		if len(failures) != 0 {
			t.Errorf("failures = %v, want none", failures)
		}
	})

	t.Run("a shorter count does not match a longer one", func(t *testing.T) {
		digest := "## AI\n- Headcount is 15 (@c https://x.com/c/status/3)"
		failures, _ := evalDigest(digest, kept, topics)
		if len(failures) != 1 || !strings.Contains(failures[0], "magnitude 15") {
			t.Errorf("failures = %v, want magnitude 15", failures)
		}
	})
	t.Run("digits inside a handle are not counts", func(t *testing.T) {
		kept := []sources.Tweet{
			{URL: "https://x.com/Breaking911/status/1", Text: "Revenue grew 20%"},
		}
		digest := "## AI\n- Revenue grew 20% (@Breaking911 https://x.com/Breaking911/status/1)"
		failures, _ := evalDigest(digest, kept, topics)
		if len(failures) != 0 {
			t.Errorf("failures = %v, want none (911 is part of the handle)", failures)
		}
	})

	t.Run("spaced thousands are one count", func(t *testing.T) {
		kept := []sources.Tweet{
			{URL: "https://x.com/p/status/2", Text: "Claims fell to 194 000"},
		}
		digest := "## AI\n- Claims fell to 194 000 (@p https://x.com/p/status/2)"
		failures, _ := evalDigest(digest, kept, topics)
		if len(failures) != 0 {
			t.Errorf("failures = %v, want none (194 000 is 194000, not 0)", failures)
		}
	})

	t.Run("a status id is not a count", func(t *testing.T) {
		kept := []sources.Tweet{
			{URL: "https://x.com/a/status/1", Text: "No figures here"},
		}
		digest := "## AI\n- No figures here 2113250177712841069 (@a https://x.com/a/status/1)"
		failures, _ := evalDigest(digest, kept, topics)
		if len(failures) != 0 {
			t.Errorf("failures = %v, want none (19 digits is a status id)", failures)
		}
	})
}
