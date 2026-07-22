package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type echoInput struct {
	Msg string `json:"msg" jsonschema:"text to echo back"`
}
type echoOutput struct {
	Echo string `json:"echo"`
}

func TestMCPToolsAdapter(t *testing.T) {
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "v0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "echo", Description: "echoes the message"},
		func(ctx context.Context, req *mcp.CallToolRequest, in echoInput) (*mcp.CallToolResult, echoOutput, error) {
			return nil, echoOutput{Echo: "heard: " + in.Msg}, nil
		})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	adapter := MCPTools{Session: session}
	defs, err := adapter.Tools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Name != "echo" || !strings.Contains(string(defs[0].InputSchema), `"msg"`) {
		t.Errorf("defs = %+v, want echo with its schema", defs)
	}
	out, isErr, err := adapter.Call(ctx, "echo", json.RawMessage(`{"msg":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if isErr || !strings.Contains(out, "heard: hi") {
		t.Errorf("out = %q isErr = %v, want the echoed text", out, isErr)
	}
}
