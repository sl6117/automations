package weeklydeepdive

import (
	"strings"
	"testing"
)

func TestRenderBrief(t *testing.T) {
	b := Brief{
		Title:   "OpenRouter shift",
		Summary: "A lede with " + HedgeLabel + ".",
		Sections: []BriefSection{
			{Heading: "What we know", Body: "Usage hit 58% (" + HedgeLabel + ")."},
		},
	}
	got := renderBrief(b, EditorReport{Pass: true, Failures: []string{}})
	for _, want := range []string{
		"# OpenRouter shift",
		"## What we know",
		HedgeLabel,
		"_Editor: pass (contract check)._",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("render missing %q\n%s", want, got)
		}
	}

	fail := renderBrief(b, EditorReport{Pass: false, Failures: []string{"missing hedge on claim X"}})
	if !strings.Contains(fail, "_Editor: fail — missing hedge on claim X_") {
		t.Errorf("fail footer = %q", fail)
	}
}
