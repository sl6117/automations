// package sinks defines the output interface for automations. Concrete sinks
// (console now; telegram later) implement Sink. A sink only delivers a finished message
// the caller is responsible for formatting its content into that string
package sinks

import "context"

// Sink is a swappable delivery destination. Mirrors Source: a Name plus one verb
type Sink interface {
	Name() string
	Deliver(ctx context.Context, message string) error
}
