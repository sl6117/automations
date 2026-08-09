package weeklydeepdive

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/sl6117/automations/internal/agent"
	"github.com/sl6117/automations/internal/ai"
)

// serialTools serializes ToolSource calls - MCP sessions are not assumed concurrent-safe.
type serialTools struct {
	mu    sync.Mutex
	inner agent.ToolSource
}

func (s *serialTools) Tools(ctx context.Context) ([]ai.ToolDef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Tools(ctx)
}

func (s *serialTools) Call(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Call(ctx, name, args)
}
