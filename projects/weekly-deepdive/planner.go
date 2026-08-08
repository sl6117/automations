package weeklydeepdive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sl6117/automations/internal/agent"
	"github.com/sl6117/automations/internal/ai"
)

const plannerSubmitTool = "submit_plan"

var planSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
	  "story": {"type": "string"},
	  "whyChosen": {"type": "string"},
	  "sourceTweetIDs": {"type": "array", "items": {"type": "string"}},
	  "researchQuestions": {"type": "array", "items": {"type": "string"}}
	},
	"required": ["story", "whyChosen", "sourceTweetIDs", "researchQuestions"]
  }`)

// Plan is the planner role's contract. Downstream roles trust these fields, not prose.
type Plan struct {
	Story             string   `json:"story"`
	WhyChosen         string   `json:"whyChosen"`
	SourceTweetIDs    []string `json:"sourceTweetIDs"`
	ResearchQuestions []string `json:"researchQuestions"`
}

// stringList accepts a JSON array of strings or a single string (models often slip and send one id as a string)
// empty string -> empty list
type stringList []string

func planWeek(ctx context.Context, cfg agent.Config, now time.Time) (Plan, agent.Result, error) {
	since := now.UTC().AddDate(0, 0, -7).Format("2006-01-02")
	prompt := fmt.Sprintf(
		"Pick the single biggest story from digests since %s (rolling 7 days). Use the tools, then call %s with the plan.",
		since, plannerSubmitTool,
	)

	cfg.OutputTool = &ai.ToolDef{
		Name:        plannerSubmitTool,
		Description: "Submit the weekly deep-dive plan",
		InputSchema: planSchema,
	}
	res, err := agent.Run(ctx, cfg, prompt)
	if err != nil {
		return Plan{}, res, err
	}
	plan, err := parsePlan(json.RawMessage(res.Text))
	return plan, res, err
}

func parsePlan(raw json.RawMessage) (Plan, error) {
	// pointers: missing field = error, not silent zero
	var got struct {
		Story             *string     `json:"story"`
		WhyChosen         *string     `json:"whyChosen"`
		SourceTweetIDs    *stringList `json:"sourceTweetIDs"`
		ResearchQuestions *stringList `json:"researchQuestions"`
	}

	if err := json.Unmarshal(raw, &got); err != nil {
		return Plan{}, fmt.Errorf("parse plan: %w", err)
	}
	if got.Story == nil || got.WhyChosen == nil || got.SourceTweetIDs == nil || got.ResearchQuestions == nil {
		return Plan{}, fmt.Errorf("plan missing required fields")
	}
	if strings.TrimSpace(*got.Story) == "" || strings.TrimSpace(*got.WhyChosen) == "" {
		return Plan{}, fmt.Errorf("plan story/whyChosen must be non-empty")
	}
	if len(*got.ResearchQuestions) == 0 {
		return Plan{}, fmt.Errorf("plan researchQuestions must be non-empty")
	}
	return Plan{
		Story:             *got.Story,
		WhyChosen:         *got.WhyChosen,
		SourceTweetIDs:    []string(*got.SourceTweetIDs),
		ResearchQuestions: []string(*got.ResearchQuestions),
	}, nil
}

func (s *stringList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*s = nil
		return nil
	}
	if data[0] == '"' {
		var one string
		if err := json.Unmarshal(data, &one); err != nil {
			return err
		}
		one = strings.TrimSpace(one)
		if one == "" {
			*s = nil
			return nil
		}
		*s = []string{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*s = many
	return nil
}
