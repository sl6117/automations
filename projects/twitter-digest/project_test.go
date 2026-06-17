package twitterdigest

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/runner"
)

func TestProjectRun(t *testing.T) {
	var buf bytes.Buffer
	runTime := &runner.Runtime{
		DryRun:     true,
		Log:        log.New(&buf, "", 0),
		ProjectDir: ".",
	}

	if err := (&project{}).Run(context.Background(), runTime); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	got := buf.String()

	for _, want := range []string{"Daily X Digest", "## AI", "Dario"} {
		if !strings.Contains(got, want) {
			t.Errorf("digest missing %q\n--- output ---\n%s", want, got)
		}
	}

	if strings.Contains(got, "@cryptomoonboy") {
		t.Errorf("digest contains spam handle\n--- output ---\n%s", got)
	}

}
