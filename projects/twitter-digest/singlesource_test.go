package twitterdigest

import (
	"strings"
	"testing"

	"github.com/sl6117/automations/pkg/sources"
)

// Counting is by status id -> attributed handle, mirroring citations.go:
// the model mangles handles (case-folding, translation) but never ids.
func TestAnnotateSingleSource(t *testing.T) {
	kept := []sources.Tweet{
		{ID: "1", Handle: "alice", Text: "original scoop"},
		{ID: "2", Handle: "alice", Text: "follow-up detail"},
		{ID: "3", Handle: "bob", Text: "independent confirmation"},
		// two different posters retweeting the same voice: one source, not two
		{ID: "4", Handle: "alice", Text: "RT @carol: exclusive report"},
		{ID: "5", Handle: "bob", Text: "RT @carol: exclusive report"},
		// carol's own post; id 4/5 RTs must merge with it, not add to it
		{ID: "6", Handle: "carol", Text: "exclusive report"},
		{ID: "7", Handle: "Dave", Text: "mixed-case handle"},
	}
	cases := []struct {
		name string
		line string
		want bool // labeled?
	}{
		{
			name: "one citation is single-source",
			line: "- Alice reported X (@alice https://x.com/alice/status/1)",
			want: true,
		},
		{
			name: "two citations same handle still single-source",
			line: "- Alice reported X twice (@alice https://x.com/alice/status/1) (@alice https://x.com/alice/status/2)",
			want: true,
		},
		{
			name: "two distinct handles are corroborated",
			line: "- Confirmed by both (@alice https://x.com/alice/status/1) (@bob https://x.com/bob/status/3)",
			want: false,
		},
		{
			name: "two RTs of the same voice are one source",
			line: "- Carol's report amplified (@alice https://x.com/alice/status/4) (@bob https://x.com/bob/status/5)",
			want: true,
		},
		{
			name: "RT plus the original it retweets is one source",
			line: "- Carol's report (@carol https://x.com/carol/status/6) (@alice https://x.com/alice/status/4)",
			want: true,
		},
		{
			name: "RT plus an unrelated voice is corroborated",
			line: "- Two voices (@alice https://x.com/alice/status/4) (@bob https://x.com/bob/status/3)",
			want: false,
		},
		{
			name: "no citation means unknown, not single-source",
			line: "- A bullet where the model dropped the citation",
			want: false,
		},
		{
			name: "hallucinated id resolves to no known source",
			line: "- Invented citation (@ghost https://x.com/ghost/status/999)",
			want: false,
		},
		{
			name: "model case-folds the handle but the id still resolves",
			line: "- Dave's post (@dave https://x.com/dave/status/7)",
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := annotateSingleSource(tc.line, kept)
			labeled := strings.HasSuffix(got, singleSourceLabel)
			if labeled != tc.want {
				t.Errorf("labeled = %v, want %v\ngot: %q", labeled, tc.want, got)
			}
			if !labeled && got != tc.line {
				t.Errorf("unlabeled line must be unchanged\ngot:  %q\nwant: %q", got, tc.line)
			}
		})
	}
}

// The label may only ever touch bullet lines: headers route sections to
// subscribers and must stay byte-identical, and prose lines are not stories.
func TestAnnotateSingleSourceOnlyTouchesBullets(t *testing.T) {
	kept := []sources.Tweet{
		{ID: "1", Handle: "alice", Text: "scoop"},
		{ID: "3", Handle: "bob", Text: "confirmation"},
	}
	digest := strings.Join([]string{
		"## Tech",
		"- Single (@alice https://x.com/alice/status/1)",
		"- Corroborated (@alice https://x.com/alice/status/1) (@bob https://x.com/bob/status/3)",
		"## Other",
		"not a bullet, even with a link https://x.com/alice/status/1",
	}, "\n")

	got := annotateSingleSource(digest, kept)

	want := strings.Join([]string{
		"## Tech",
		"- Single (@alice https://x.com/alice/status/1)" + singleSourceLabel,
		"- Corroborated (@alice https://x.com/alice/status/1) (@bob https://x.com/bob/status/3)",
		"## Other",
		"not a bullet, even with a link https://x.com/alice/status/1",
	}, "\n")
	if got != want {
		t.Errorf("digest mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}
