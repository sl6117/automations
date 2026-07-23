package weeklydeepdive

import (
	"strings"
	"testing"
)

func TestParseEditorReport(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr string
		pass    bool
	}{
		{
			name: "pass",
			in:   `{"pass":true,"failures":[]}`,
			pass: true,
		},
		{
			name: "fail with reasons",
			in:   `{"pass":false,"failures":["asserted Qeshm strike without hedge"]}`,
		},
		{
			name:    "missing failures",
			in:      `{"pass":true}`,
			wantErr: "missing required fields",
		},
		{
			name:    "pass with failures",
			in:      `{"pass":true,"failures":["x"]}`,
			wantErr: "inconsistent",
		},
		{
			name:    "fail with empty failures",
			in:      `{"pass":false,"failures":[]}`,
			wantErr: "inconsistent",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseEditorReport(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Pass != tc.pass {
				t.Errorf("Pass = %v, want %v", got.Pass, tc.pass)
			}
		})
	}
}
