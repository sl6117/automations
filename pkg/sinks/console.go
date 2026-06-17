package sinks

import (
	"context"
	"fmt"
	"io"
	"os"
)

type Console struct {
	Out io.Writer
}

func (c Console) Name() string { return "console" }

func (c Console) Deliver(ctx context.Context, message string) error {
	out := c.Out
	if out == nil {
		out = os.Stdout
	}
	_, err := fmt.Fprintln(out, message)

	return err
}
