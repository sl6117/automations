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

	var res Result
	messages := []ai.Message{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: prompt}}}}

	for {
		resp, err := cfg.Client.Chat(ctx, ai.ChatRequest{
			Model:     cfg.Model,
			System:    cfg.System,
			Messages:  messages,
			Tools:     tools,
			MaxTokens: cfg.MaxTokens,
		})
		if err != nil {
			return Result{}, fmt.Errorf("chat: %w", err)
		}
		res.Usage.InputTokens += resp.Usage.InputTokens
		res.Usage.OutputTokens += resp.Usage.OutputTokens

		// the FULL assistant content goes back into the conversation -
		// dropping a block would orphan its tool_use_id (API rejects that)
		messages = append(messages, ai.Message{Role: "assistant", Content: resp.Content})

		switch resp.StopReason {
		case "tool_use":
			// fall through to execute below
		case "max_tokens":
			// the reply itself was cut off mid-thought: unusable, real breakage
			return Result{}, fmt.Errorf("reply truncated by max_tokens; raise Config.MaxTokens")
		default: // "end_turn": the model answered
			res.Text = textOf(resp.Content)
			return res, nil
		}

		if res.ToolTurns >= cfg.MaxToolTurns {
			return finalAnswer(ctx, cfg, messages, resp.Content, res)
		}
		res.ToolTurns++

		var results []ai.ContentBlock
		for _, b := range resp.Content {
			if b.Type != "tool_use" {
				continue
			}
			out, isErr, err := cfg.Tools.Call(ctx, b.Name, b.Input)
			if err != nil {
				return Result{}, fmt.Errorf("call %s: %w", b.Name, err)
			}
			results = append(results, ai.ContentBlock{Type: "tool_result", ToolUseID: b.ID, Content: out, IsError: isErr})
		}
		messages = append(messages, ai.Message{Role: "user", Content: results})
	}
}

// finalAnswer is the escape hatch: answer the pending tool_use blocks with budget-exhausted refusals,
// then ask the model to answer from what it has,
// offering NO tools so it cannot ask again.
func finalAnswer(ctx context.Context, cfg Config, messages []ai.Message, pending []ai.ContentBlock, res Result) (Result, error) {
	var blocks []ai.ContentBlock
	for _, b := range pending {
		if b.Type == "tool_use" {
			blocks = append(blocks, ai.ContentBlock{Type: "tool_result", ToolUseID: b.ID, Content: "tool budget exhausted; call not executed", IsError: true})
		}
	}
	blocks = append(blocks, ai.ContentBlock{Type: "text", Text: "The tool budget is exhausted. Answer the original question now using only what you have already gathered."})

	messages = append(messages, ai.Message{Role: "user", Content: blocks})

	resp, err := cfg.Client.Chat(ctx, ai.ChatRequest{
		Model:     cfg.Model,
		System:    cfg.System,
		Messages:  messages,
		MaxTokens: cfg.MaxTokens,
	})
	if err != nil {
		return Result{}, fmt.Errorf("final answer: %w", err)
	}
	res.Usage.InputTokens += resp.Usage.InputTokens
	res.Usage.OutputTokens += resp.Usage.OutputTokens
	res.Text = textOf(resp.Content)
	res.Truncated = true
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
