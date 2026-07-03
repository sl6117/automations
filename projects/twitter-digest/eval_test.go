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

}
