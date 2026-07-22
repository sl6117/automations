// ask runs one agent loop against the digest-mcp server: it launches the server as a subprocess,
// hands its tools to Haiku, and prints the answer.
// Usage (from the repo root): go run ./cmd/ask "which digests failed this week?"
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sl6117/automations/internal/agent"
	"github.com/sl6117/automations/internal/ai"
)

func main() {

	if len(os.Args) < 2 {
		log.Fatal(`usage: ask "<question>"`)
	}
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "ask", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command("bin/digest-mcp")}, nil)
	if err != nil {
		log.Fatalf("connect digest-mcp: %v", err)
	}
	defer session.Close()
	res, err := agent.Run(ctx, agent.Config{
		Client:       ai.Anthropic{APIKey: os.Getenv("ANTHROPIC_API_KEY")},
		Tools:        agent.MCPTools{Session: session},
		Model:        "claude-haiku-4-5",
		System:       "You answer questions about the twitter-digest automation's run archive using the available tools. Be concise; cite artifact keys or months when relevant.",
		MaxTokens:    1000,
		MaxToolTurns: 5,
	}, os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Text)
	if res.Truncated {
		fmt.Println("\n[truncated: tool budget exhausted]")
	}
	fmt.Printf("\n(%d tool turns, %d in / %d out tokens)\n", res.ToolTurns, res.Usage.InputTokens, res.Usage.OutputTokens)
}
