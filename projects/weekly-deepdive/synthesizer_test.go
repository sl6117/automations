package weeklydeepdive

import (
	"strings"
	"testing"
)

func TestParseBrief(t *testing.T) {
	valid := `{
		"title": "OpenRouter shift",
		"summary": "Chinese models lead US firm usage on OpenRouter.",
		"sections": [
			{"heading": "What we know", "body": "Usage hit 58% (reported but not corroborated)."}
		]
	}`
	cases := []struct {
		name    string
		in      string
		wantErr string
	}{
		{name: "happy path", in: valid},
		{name: "markdown fence", in: "```json\n" + valid + "\n```"},
		{name: "missing sections", in: `{"title":"t","summary":"s"}`, wantErr: "missing required fields"},
		{name: "empty sections", in: `{"title":"t","summary":"s","sections":[]}`, wantErr: "sections must be non-empty"},
		{name: "empty section body", in: `{"title":"t","summary":"s","sections":[{"heading":"h","body":"  "}]}`, wantErr: "heading/body must be non-empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseBrief(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Title == "" || len(got.Sections) == 0 {
				t.Errorf("brief = %+v", got)
			}
			if !strings.Contains(got.Sections[0].Body, HedgeLabel) {
				t.Errorf("body should include hedge label %q", HedgeLabel)
			}
		})
	}
}
