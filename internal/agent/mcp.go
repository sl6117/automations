package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sl6117/automations/internal/ai"
)

// MCPTools adapts an MCP client session to the ToolSource interface, so the agent loop
// can use any MCP server's tools without knowing the protocol.
type MCPTools struct {
	Session *mcp.ClientSession
}

func (m MCPTools) Tools(ctx context.Context) ([]ai.ToolDef, error) {
	res, err := m.Session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp list tools: %w", err)
	}
	var defs []ai.ToolDef
	for _, t := range res.Tools {
		schema, err := json.Marshal(t.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("marshal schema for %s: %w", t.Name, err)
		}
		defs = append(defs, ai.ToolDef{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	return defs, nil
}

func (m MCPTools) Call(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	res, err := m.Session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", false, fmt.Errorf("mcp call %s: %w", name, err)
	}

	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n"), res.IsError, nil
}
