package weeklydeepdive

import (
	"encoding/json"
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
			name: "sourceTweetIDs as single string",
			in: `{
				"story": "x",
				"whyChosen": "y",
				"sourceTweetIDs": "111",
				"researchQuestions": ["q1"]
			}`,
			check: func(t *testing.T, p Plan) {
				if len(p.SourceTweetIDs) != 1 || p.SourceTweetIDs[0] != "111" {
					t.Fatalf("SourceTweetIDs = %v, want [111]", p.SourceTweetIDs)
				}
			},
		},
		{
			name: "sourceTweetIDs empty string means none",
			in: `{
				"story": "x",
				"whyChosen": "y",
				"sourceTweetIDs": "",
				"researchQuestions": ["q1"]
			}`,
			check: func(t *testing.T, p Plan) {
				if len(p.SourceTweetIDs) != 0 {
					t.Fatalf("SourceTweetIDs = %v, want empty", p.SourceTweetIDs)
				}
			},
		},
		{
			name:    "invalid json",
			in:      `{`,
			wantErr: "parse plan",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePlan(json.RawMessage(tc.in))
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
