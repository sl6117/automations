package hello

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/runner"
)

func TestHelloRun(t *testing.T) {
	tests := []struct {
		name   string
		dryRun bool
		want   string
	}{
		{name: "normal", dryRun: false, want: "real"},
		{name: "dry-run", dryRun: true, want: "dry-run"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var buf bytes.Buffer
			runTime := &runner.Runtime{
				DryRun: test.dryRun,
				Log:    log.New(&buf, "", 0),
			}

			if err := (&project{}).Run(context.Background(), runTime); err != nil {
				t.Fatalf("Error during Run: %v", err)
			}
			got := buf.String()

			if !strings.Contains(got, test.want) {
				t.Errorf("dryRun=%v: got %q, want %q", test.dryRun, got, test.want)
			}
		})
	}
}
