package weeklydeepdive

import (
	"strings"
	"testing"
)

func TestParsePlan(t *testing.T) {
	valid := `{
		"story": "FIFA adds a year without a source",
		"whyChosen": "Clear faithfulness fail with a checkable claim",
		"sourceTweetIDs": ["111", "222"],
		"researchQuestions": ["What did the source actually say about the date?"]
	}`
	cases := []struct {
		name    string
		in      string
		wantErr string
		check   func(t *testing.T, p Plan)
	}{
		{
			name: "happy path",
			in:   valid,
			check: func(t *testing.T, p Plan) {
				if p.Story == "" || p.WhyChosen == "" || len(p.SourceTweetIDs) != 2 || len(p.ResearchQuestions) != 1 {
					t.Errorf("plan = %+v", p)
				}
			},
		},
		{
			name: "markdown fence wrapping",
			in:   "Here you go:\n```json\n" + valid + "\n```\n",
			check: func(t *testing.T, p Plan) {
				if p.Story != "FIFA adds a year without a source" {
					t.Errorf("story = %q", p.Story)
				}
			},
		},
		{
			name:    "missing field",
			in:      `{"story":"x","whyChosen":"y","sourceTweetIDs":["1"]}`,
			wantErr: "missing required fields",
		},
		{
			name: "empty researchQuestions",
			in: `{
				"story": "x",
				"whyChosen": "y",
				"sourceTweetIDs": ["1"],
				"researchQuestions": []
			}`,
			wantErr: "researchQuestions must be non-empty",
		},
		{
			name:    "no json",
			in:      "sorry, no plan today",
			wantErr: "no JSON found",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePlan(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}
