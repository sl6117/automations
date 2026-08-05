// package agent runs a model-with-tools loop: send the conversation, execute the model's tool calls,
// feed results back, repeat until the model answers or the budget forces a final answer.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sl6117/automations/internal/ai"
)

// ToolSource supplies tool definitions and executes calls. Satisfied by an MCP session adapter;
// tests inject a fake.
type ToolSource interface {
	Tools(ctx context.Context) ([]ai.ToolDef, error)
	// Call executes one tool. isError reports a tool-level failure the model
	// should see (and may recover from); err reports transport breakage
	// that aborts the run.
	Call(ctx context.Context, name string, args json.RawMessage) (result string, isError bool, err error)
}

type Config struct {
	Client    ai.ChatClient
	Tools     ToolSource
	Model     string
	System    string
	MaxTokens int
	// MaxToolTurns bounds how many times the model may request tools before
	// the loop forces a final answer from what was gathered (the escape hatch).
	MaxToolTurns int
	// WebSearchMaxUses enables Anthropic server-side web_search when > 0.
	// The API executes the search; the loop never calls it locally.
	WebSearchMaxUses int
	// FetchAllowlist, when set, is filled from web_search results each turn and should
	// also wrap Tools via GatedFetch so invented URLs cannot be fetched.
	FetchAllowlist *Allowlist
	// OnToolCall, if set, is invoked after each tool Call with the raw args and outcome.
	// Intended for stderr/observability; must not mutate the loop.
	OnToolCall func(name string, args json.RawMessage, result string, isError bool)
	// PromptCache asks the Chat client to enable Anthropic automatic prompt caching on every turn.
	// Off by default; deepdive agent roles opt in.
	PromptCache bool
	// OutputTool, when set, is offered alongside Tools every turn. When the model calls it,
	// the loop ends successfully with Result.Text = tool input JSON and doesn't invoke ToolSource.Call.
	// Used for schema-forced structured output (e.g. submit_plan) while real tools still run during research turns.
	// Nil preserves end_turn text answers (legacy / roles without a submit tool).
	OutputTool *ai.ToolDef
}

// Result always carries usable text; Truncated marks answers the budget cut short
// so callers can hedge rather than trust them as complete.
type Result struct {
	Text      string
	Truncated bool
	ToolTurns int
	Usage     ai.Usage
}

func Run(ctx context.Context, cfg Config, prompt string) (Result, error) {
	tools, err := cfg.Tools.Tools(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list tools: %w", err)
	}
	offer := tools
	if cfg.OutputTool != nil {
		offer = append(append([]ai.ToolDef{}, tools...), *cfg.OutputTool)
	}

	var res Result
	messages := []ai.Message{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: prompt}}}}

	for {
		chatReq := ai.ChatRequest{
			Model:       cfg.Model,
			System:      cfg.System,
			Messages:    messages,
			Tools:       offer,
			MaxTokens:   cfg.MaxTokens,
			PromptCache: cfg.PromptCache,
		}
		if cfg.WebSearchMaxUses > 0 {
			chatReq.ServerTools = []ai.ServerTool{{
				Type:    ai.WebSearchToolType,
				Name:    "web_search",
				MaxUses: cfg.WebSearchMaxUses,
			}}
		}
		resp, err := cfg.Client.Chat(ctx, chatReq)
		if err != nil {
			return Result{}, fmt.Errorf("chat: %w", err)
		}
		res.Usage.InputTokens += resp.Usage.InputTokens
		res.Usage.OutputTokens += resp.Usage.OutputTokens
		res.Usage.CacheCreationInputTokens += resp.Usage.CacheCreationInputTokens
		res.Usage.CacheReadInputTokens += resp.Usage.CacheReadInputTokens

		// the FULL assistant content goes back into the conversation -
		// dropping a block would orphan its tool_use_id (API rejects that)
		messages = append(messages, ai.Message{Role: "assistant", Content: resp.Content})

		switch resp.StopReason {
		case "tool_use":
			// Output tool: typed return channel, before budget gate.
			if out, ok := outputToolInput(cfg.OutputTool, resp.Content); ok {
				res.Text = string(out)
				return res, nil
			}
		case "max_tokens":
			// the reply itself was cut off mid-thought: unusable, real breakage
			return Result{}, fmt.Errorf("reply truncated by max_tokens; raise Config.MaxTokens")
		default: // "end_turn": the model answered
			if cfg.OutputTool != nil {
				return Result{}, fmt.Errorf("want tool_use %s, got end_turn", cfg.OutputTool.Name)
			}
			res.Text = textOf(resp.Content)
			return res, nil
		}

		if res.ToolTurns >= cfg.MaxToolTurns {
			return finalAnswer(ctx, cfg, messages, resp.Content, res)
		}
		res.ToolTurns++
		if cfg.FetchAllowlist != nil {
			cfg.FetchAllowlist.AddFromContent(resp.Content)
		}

		var results []ai.ContentBlock
		for _, b := range resp.Content {
			if b.Type != "tool_use" {
				continue
			}
			out, isErr, err := cfg.Tools.Call(ctx, b.Name, b.Input)
			if err != nil {
				return Result{}, fmt.Errorf("call %s: %w", b.Name, err)
			}
			if cfg.OnToolCall != nil {
				cfg.OnToolCall(b.Name, b.Input, out, isErr)
			}
			results = append(results, ai.ContentBlock{Type: "tool_result", ToolUseID: b.ID, Content: out, IsError: isErr})
		}
		messages = append(messages, ai.Message{Role: "user", Content: results})

	}
}

func outputToolInput(out *ai.ToolDef, content []ai.ContentBlock) (json.RawMessage, bool) {
	if out == nil {
		return nil, false
	}
	for _, b := range content {
		if b.Type == "tool_use" && b.Name == out.Name {
			return b.Input, true
		}
	}
	return nil, false
}

// finalAnswer is the escape hatch: refuse pending tool_use blocks, then force an answer from what was gathered.
// With OutputTool set, only that tool is offered and tool_choice forces it; otherwise no tools (legacy text answer).
func finalAnswer(ctx context.Context, cfg Config, messages []ai.Message, pending []ai.ContentBlock, res Result) (Result, error) {
	var blocks []ai.ContentBlock
	for _, b := range pending {
		if b.Type == "tool_use" {
			blocks = append(blocks, ai.ContentBlock{Type: "tool_result", ToolUseID: b.ID, Content: "tool budget exhausted; call not executed", IsError: true})
		}
	}

	nudge := "The tool budget is exhausted. Answer the original question now using only what you have already gathered."
	if cfg.OutputTool != nil {
		nudge = fmt.Sprintf("The tool budget is exhausted. Call %s now with your best answer from what you have already gathered.", cfg.OutputTool.Name)
	}
	blocks = append(blocks, ai.ContentBlock{Type: "text", Text: nudge})

	messages = append(messages, ai.Message{Role: "user", Content: blocks})

	chatReq := ai.ChatRequest{
		Model:       cfg.Model,
		System:      cfg.System,
		Messages:    messages,
		MaxTokens:   cfg.MaxTokens,
		PromptCache: cfg.PromptCache,
	}
	if cfg.OutputTool != nil {
		chatReq.Tools = []ai.ToolDef{*cfg.OutputTool}
		chatReq.ToolChoice = &ai.ToolChoice{Type: "tool", Name: cfg.OutputTool.Name}
	}

	resp, err := cfg.Client.Chat(ctx, chatReq)
	if err != nil {
		return Result{}, fmt.Errorf("final answer: %w", err)
	}
	res.Usage.InputTokens += resp.Usage.InputTokens
	res.Usage.OutputTokens += resp.Usage.OutputTokens
	res.Usage.CacheCreationInputTokens += resp.Usage.CacheCreationInputTokens
	res.Usage.CacheReadInputTokens += resp.Usage.CacheReadInputTokens

	res.Truncated = true

	if cfg.OutputTool != nil {
		if out, ok := outputToolInput(cfg.OutputTool, resp.Content); ok {
			res.Text = string(out)
			return res, nil
		}
		return Result{}, fmt.Errorf("final answer: want tool_use %s, got %q", cfg.OutputTool.Name, resp.StopReason)
	}
	res.Text = textOf(resp.Content)
	return res, nil
}

func textOf(content []ai.ContentBlock) string {
	var parts []string
	for _, b := range content {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}
