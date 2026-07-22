package weeklydeepdive

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sl6117/automations/internal/agent"
)

// ResearchReport is one researcher's contract. corroborated=false is valid output
// (couldn't check is NOT false claim); the synthesizer hedges, it doesn't drop.
type ResearchReport struct {
	Question     string   `json:"question"`
	Findings     []string `json:"findings"`
	Sources      []string `json:"sources"`
	Corroborated bool     `json:"corroborated"`
}

func researchOne(ctx context.Context, cfg agent.Config, story, question string) (ResearchReport, agent.Result, error) {
	prompt := fmt.Sprintf(
		"Story under investigation:\n%s\n\nResearch question:\n%s\n\nUse fetch_url (and archive tools if needed). Reply with ONLY a JSON object matching the schema in the system prompt. If you cannot verify, set corroborated=false — that is a valid answer.",
		story, question,
	)
	res, err := agent.Run(ctx, cfg, prompt)
	if err != nil {
		return ResearchReport{}, res, err
	}
	report, err := parseResearchReport(res.Text)
	return report, res, err
}

func parseResearchReport(text string) (ResearchReport, error) {
	raw, err := extractJSON(text)
	if err != nil {
		return ResearchReport{}, err
	}
	var got struct {
		Question     *string   `json:"question"`
		Findings     *[]string `json:"findings"`
		Sources      *[]string `json:"sources"`
		Corroborated *bool     `json:"corroborated"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		return ResearchReport{}, fmt.Errorf("parse report: %w", err)
	}
	if got.Question == nil || got.Findings == nil || got.Sources == nil || got.Corroborated == nil {
		return ResearchReport{}, fmt.Errorf("report missing required fields")
	}
	if strings.TrimSpace(*got.Question) == "" {
		return ResearchReport{}, fmt.Errorf("research report question must be non-empty")
	}
	//empty findings/sources + corroborated=false is explicitly allowed
	return ResearchReport{
		Question:     *got.Question,
		Findings:     *got.Findings,
		Sources:      *got.Sources,
		Corroborated: *got.Corroborated,
	}, nil
}
