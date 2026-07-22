package weeklydeepdive

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sl6117/automations/internal/agent"
	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/internal/runner"
)

func init() {
	runner.Register(&project{})
}

type project struct{}

func (p *project) Name() string { return "weekly-deepdive" }

func (p *project) Run(ctx context.Context, rt *runner.Runtime) error {
	system, err := os.ReadFile(filepath.Join(rt.ProjectDir, "prompts", "planner.md"))
	if err != nil {
		return fmt.Errorf("read planner prompt: %w", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "weekly-deepdive", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command("bin/digest-mcp")}, nil)
	if err != nil {
		return fmt.Errorf("connect digest-mcp: %w", err)
	}
	defer session.Close()

	plan, res, err := planWeek(ctx, agent.Config{
		Client:       ai.Anthropic{APIKey: os.Getenv("ANTHROPIC_API_KEY")},
		Tools:        agent.MCPTools{Session: session},
		Model:        "claude-haiku-4-5",
		System:       string(system),
		MaxTokens:    1000,
		MaxToolTurns: 5,
		OnToolCall: func(name string, args json.RawMessage, result string, isError bool) {
			rt.Log.Printf("tool %s args=%s result_bytes=%d isError=%v", name, string(args), len(result), isError)
		},
	}, time.Now())

	if err != nil {
		return err
	}
	if res.Truncated {
		return fmt.Errorf("planner truncated: tool budget exhausted before a complete plan")
	}

	out, err := json.MarshalIndent(plan, "", " ")
	if err != nil {
		return err
	}
	rt.Log.Printf("plan:\n%s", out)
	rt.Log.Printf("(%d tool turns, %d in / %d out tokens)", res.ToolTurns, res.Usage.InputTokens, res.Usage.OutputTokens)
	if !rt.DryRun {
		rt.Log.Println("delivery not wired yet; plan only")
	}
	return nil
}
