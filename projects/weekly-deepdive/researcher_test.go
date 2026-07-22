package weeklydeepdive

import (
	"strings"
	"testing"
)

func TestParseResearchReport(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr string
		check   func(t *testing.T, r ResearchReport)
	}{
		{
			name: "happy corroborated",
			in: `{
				"question": "What was the impact?",
				"findings": ["FT reports $X loss"],
				"sources": ["https://example.com/ft"],
				"corroborated": true
			}`,
			check: func(t *testing.T, r ResearchReport) {
				if !r.Corroborated || len(r.Findings) != 1 || len(r.Sources) != 1 {
					t.Errorf("report = %+v", r)
				}
			},
		},
		{
			name: "uncertainty is valid",
			in: `{
				"question": "What was the impact?",
				"findings": [],
				"sources": [],
				"corroborated": false
			}`,
			check: func(t *testing.T, r ResearchReport) {
				if r.Corroborated {
					t.Errorf("want corroborated=false, got %+v", r)
				}
			},
		},
		{
			name:    "missing corroborated",
			in:      `{"question":"q","findings":[],"sources":[]}`,
			wantErr: "missing required fields",
		},
		{
			name:    "empty question",
			in:      `{"question":"  ","findings":[],"sources":[],"corroborated":false}`,
			wantErr: "question must be non-empty",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseResearchReport(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, got)
		})
	}
}
