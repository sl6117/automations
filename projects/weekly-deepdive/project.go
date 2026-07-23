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
	"github.com/sl6117/automations/internal/config"
	"github.com/sl6117/automations/internal/runner"
	"github.com/sl6117/automations/pkg/sinks"
)

const (
	plannerModel         = "claude-haiku-4-5"
	researcherModel      = "claude-haiku-4-5"
	synthesizerModel     = "claude-haiku-4-5"
	roleMaxTokens        = 1000
	roleMaxToolTurns     = 5
	synthesizerMaxTokens = 2500
	maxResearchQuestions = 3 // cost guard for early dry-run
)

func init() {
	runner.Register(&project{})
}

type project struct {
	// nil -> real deps from env / bin/digest-mcp. Tests inject fakes.
	chat  ai.ChatClient
	tools agent.ToolSource
	now   func() time.Time
	sinks []sinks.Sink // nil -> selectSinks from config (dry-run: console only)
}

func (p *project) Name() string { return "weekly-deepdive" }

func (p *project) Run(ctx context.Context, rt *runner.Runtime) error {

	var cfg Config
	if err := config.Load(filepath.Join(rt.ProjectDir, "config.json"), &cfg); err != nil {
		return err
	}

	plannerSys, err := os.ReadFile(filepath.Join(rt.ProjectDir, "prompts", "planner.md"))
	if err != nil {
		return fmt.Errorf("read planner prompt: %w", err)
	}
	researcherSys, err := os.ReadFile(filepath.Join(rt.ProjectDir, "prompts", "researcher.md"))
	if err != nil {
		return fmt.Errorf("read researcher prompt: %w", err)
	}
	synthesizerSys, err := os.ReadFile(filepath.Join(rt.ProjectDir, "prompts", "synthesizer.md"))
	if err != nil {
		return fmt.Errorf("read synthesizer prompt: %w", err)
	}
	editorSys, err := os.ReadFile(filepath.Join(rt.ProjectDir, "prompts", "editor.md"))
	if err != nil {
		return fmt.Errorf("read editor prompt: %w", err)
	}

	chat := p.chat
	if chat == nil {
		chat = ai.Anthropic{APIKey: os.Getenv("ANTHROPIC_API_KEY")}
	}

	tools := p.tools
	if tools == nil {
		client := mcp.NewClient(&mcp.Implementation{Name: "weekly-deepdive", Version: "v0.1.0"}, nil)
		session, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command("bin/digest-mcp")}, nil)
		if err != nil {
			return fmt.Errorf("connect digest-mcp: %w", err)
		}
		defer session.Close()
		tools = agent.MCPTools{Session: session}
	}
	now := time.Now
	if p.now != nil {
		now = p.now
	}

	onTool := func(name string, args json.RawMessage, result string, isError bool) {
		rt.Log.Printf("tool %s args=%s result_bytes=%d isError=%v", name, string(args), len(result), isError)
	}

	plan, res, err := planWeek(ctx, agent.Config{
		Client: chat, Tools: tools, Model: plannerModel,
		System: string(plannerSys), MaxTokens: roleMaxTokens, MaxToolTurns: roleMaxToolTurns,
		OnToolCall: onTool,
	}, now())

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

	questions := plan.ResearchQuestions

	if maxQ := cfg.maxQuestions(); len(questions) > maxQ {
		rt.Log.Printf("research: capping %d questions to %d (cost guard)", len(questions), maxQ)
		questions = questions[:maxQ]
	}

	var reports []ResearchReport

	for i, q := range questions {
		rt.Log.Printf("research %d/%d: %s", i+1, len(questions), q)
		report, rres, err := researchOne(ctx, agent.Config{
			Client: chat, Tools: tools, Model: researcherModel,
			System: string(researcherSys), MaxTokens: roleMaxTokens, MaxToolTurns: roleMaxToolTurns,
			OnToolCall: onTool,
		}, plan.Story, q)
		if err != nil {
			return fmt.Errorf("research %q: %w", q, err)
		}
		if rres.Truncated {
			rt.Log.Printf("research %d truncated (budget); accepting parsed report if any", i+1)
		}
		reports = append(reports, report)
		rt.Log.Printf("research %d: corroborated=%v findings=%d sources=%d (%d turns, %d in / %d out)",
			i+1, report.Corroborated, len(report.Findings), len(report.Sources),
			rres.ToolTurns, rres.Usage.InputTokens, rres.Usage.OutputTokens)
	}

	reportOut, err := json.MarshalIndent(reports, "", " ")
	if err != nil {
		return err
	}
	rt.Log.Printf("reports:\n%s", reportOut)

	brief, usage, err := synthesize(ctx, chat, synthesizerModel, string(synthesizerSys), plan, reports)
	if err != nil {
		return err
	}
	briefOut, err := json.MarshalIndent(brief, "", " ")
	if err != nil {
		return err
	}
	rt.Log.Printf("brief:\n%s", briefOut)
	rt.Log.Printf("synthesizer: %d in / %d out tokens", usage.InputTokens, usage.OutputTokens)

	edReport, edUsage, err := editBrief(ctx, chat, string(editorSys), brief, reports)
	if err != nil {
		return err // API/parse breakage; Pass=false is not an error
	}
	edOut, err := json.MarshalIndent(edReport, "", "  ")
	if err != nil {
		return err
	}
	rt.Log.Printf("editor:\n%s", edOut)
	rt.Log.Printf("editor: pass=%v failures=%d (%d in / %d out tokens)",
		edReport.Pass, len(edReport.Failures), edUsage.InputTokens, edUsage.OutputTokens)
	if !edReport.Pass {
		rt.Log.Printf("editor: contract fails (shipping anyway): %v", edReport.Failures)
	}

	msg := renderBrief(brief, edReport)
	dest := p.sinks
	if dest == nil {
		selected, err := selectSinks(cfg, rt)
		if err != nil {
			return err
		}
		dest = selected
	}
	for _, s := range dest {
		if err := s.Deliver(ctx, msg); err != nil {
			return fmt.Errorf("deliver via %s: %w", s.Name(), err)
		}
		rt.Log.Printf("delivered via %s", s.Name())
	}
	return nil
}
