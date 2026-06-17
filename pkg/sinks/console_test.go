package sinks

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestConsoleDeliver(t *testing.T) {
	var buf bytes.Buffer
	c := Console{Out: &buf}

	if err := c.Deliver(context.Background(), "hello digest"); err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	if !strings.Contains(buf.String(), "hello digest") {
		t.Errorf("Deliver wrote %q, expected hello digest", buf.String())
	}
}
