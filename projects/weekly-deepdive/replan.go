package weeklydeepdive

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sl6117/automations/internal/ai"
)

const (
	replanModel      = "claude-haiku-4-5"
	replanMaxTokens  = 800
	replanTemp       = 0.0
	replanSubmitTool = "submit_replan"
)

var replanSchema = json.RawMessage(`{
	"type": "object",
	"properties":{
		"newResearchQuestions": {
			"type": "array",
			"items": {
				"type": "string"
			}
		},
		"rationale": {
			"type": "string"
		}
	},
	"required": ["newResearchQuestions", "rationale"]
}`)

type Replan struct {
	NewResearchQuestions []string `json:"newResearchQuestions"`
	Rationale            string   `json:"rationale"`
}

func parseReplan(raw json.RawMessage) (Replan, error) {
	var got struct {
		NewResearchQuestions *[]string `json:"newResearchQuestions"`
		Rationale            *string   `json:"rationale"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		return Replan{}, fmt.Errorf("parse replan: %w", err)
	}
	if got.NewResearchQuestions == nil || got.Rationale == nil {
		return Replan{}, fmt.Errorf("replan missing required fields")
	}
	if strings.TrimSpace(*got.Rationale) == "" {
		return Replan{}, fmt.Errorf("replan rationale must be non-empty")
	}

	// empty NewResearchQuestions is valid ("research won't help")
	return Replan{
		NewResearchQuestions: *got.NewResearchQuestions,
		Rationale:            *got.Rationale,
	}, nil
}

func replanOnce(ctx context.Context, client ai.ChatClient, system, story string, asked []string, reports []ResearchReport, failures []string) (Replan, ai.Usage, error) {
	payload, err := json.MarshalIndent(struct {
		Story    string           `json:"story"`
		Asked    []string         `json:"askedQuestions"`
		Reports  []ResearchReport `json:"reports"`
		Failures []string         `json:"editorfailures"`
	}{Story: story, Asked: asked, Reports: reports, Failures: failures}, "", "  ")
	if err != nil {
		return Replan{}, ai.Usage{}, err
	}
	prompt := fmt.Sprintf(
		"The editor rejected the brief. Decide whether new research questions would fix gaps that rewriting alone cannot. \n\n%s\n\nCall %s with your replan.",
		payload, replanSubmitTool,
	)

	resp, err := client.Chat(ctx, ai.ChatRequest{
		Model:    replanModel,
		System:   system,
		Messages: []ai.Message{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: prompt}}}},
		Tools: []ai.ToolDef{{
			Name:        replanSubmitTool,
			Description: "Submit new research questions (or an empty list) after an editor failure",
			InputSchema: replanSchema,
		}},
		ToolChoice:  &ai.ToolChoice{Type: "tool", Name: replanSubmitTool},
		MaxTokens:   replanMaxTokens,
		Temperature: replanTemp,
	})
	if err != nil {
		return Replan{}, ai.Usage{}, fmt.Errorf("replan: %w", err)
	}
	if resp.StopReason == "max_tokens" {
		return Replan{}, resp.Usage, fmt.Errorf("replan: reply truncated by max_tokens")
	}
	if resp.StopReason != "tool_use" {
		preview := resp.Text
		if len(preview) > 200 {
			preview = preview[:200] + "…"
		}
		return Replan{}, resp.Usage, fmt.Errorf("replan: want stop_reason tool_use, got %q (preview: %q)", resp.StopReason, preview)
	}

	var input json.RawMessage
	for _, b := range resp.Content {
		if b.Type == "tool_use" && b.Name == replanSubmitTool {
			input = b.Input
			break
		}
	}
	if len(input) == 0 {
		return Replan{}, resp.Usage, fmt.Errorf("replan: no %s tool_use in response", replanSubmitTool)
	}
	plan, err := parseReplan(input)
	if err != nil {
		preview := string(input)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return Replan{}, resp.Usage, fmt.Errorf("replan: %w (raw preview: %q)", err, preview)

	}
	return plan, resp.Usage, nil
}

// filterReplanQuestions drops balnks, exact duplicates of asked, then applies cap
// Cap <= 0 yields nil (nothing to run)
func filterReplanQuestions(asked, proposed []string, cap int) []string {
	if cap <= 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(asked))
	for _, q := range asked {
		seen[q] = struct{}{}
	}
	var out []string
	for _, q := range proposed {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		if _, ok := seen[q]; ok {
			continue
		}
		seen[q] = struct{}{}
		out = append(out, q)
		if len(out) >= cap {
			break
		}
	}
	return out
}
