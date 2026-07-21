package obs

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/storage"
)

func TestReport(t *testing.T) {
	// t.Setenv("AUTOMATION_ROOT", t.TempDir())
	ctx := context.Background()
	store := &storage.FS{Root: t.TempDir()}

	if _, err := LogRun(ctx, store, Run{
		Project:      "twitter-digest",
		Model:        "claude-haiku-4-5",
		InputTokens:  1_000_000,
		OutputTokens: 1_000_000,
		ItemCount:    4,
		SourceReads:  150,
	}); err != nil {
		t.Fatalf("LongRun: %v", err)
	}

	var buf bytes.Buffer
	if err := Report(ctx, store, &buf); err != nil {
		t.Fatalf("Report: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "Cost: $6.0000") {
		t.Errorf("dash-id pricing not applied (want $6.0000):\n%s", out)
	}
	if !strings.Contains(out, "claude-haiku-4-5") {
		t.Errorf("last real run missing:\n%s", out)
	}
	if !strings.Contains(out, "X reads: 150") {
		t.Errorf("reads total missing:\n%s", out)
	}
}
