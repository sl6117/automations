package weeklydeepdive

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/sl6117/automations/internal/ai"
)

// reviseBrief asks the synthesizer model for one revision pass driven by the
// editor's failure list. Callers own the loop policy: budget, re-editing, and
// whether to adopt the result.
func reviseBrief(ctx context.Context, client ai.ChatClient, system string, plan Plan, reports []ResearchReport, brief Brief, failures []string) (Brief, ai.Usage, error) {
	payload, err := json.MarshalIndent(struct {
		Plan     Plan             `json:"plan"`
		Reports  []ResearchReport `json:"reports"`
		Brief    Brief            `json:"previousBrief"`
		Failures []string         `json:"editorFailures"`
	}{Plan: plan, Reports: reports, Brief: brief, Failures: failures}, "", "  ")
	if err != nil {
		return Brief{}, ai.Usage{}, err
	}
	prompt := fmt.Sprintf(
		"Your previous brief failed the editor's contract check. Rewrite it fixing ONLY the listed editorFailures — in particular, any claim not backed by a corroborated finding must carry the hedge label %q inline. Keep everything that already complied.\n\n%s\n\nReply with ONLY a JSON object matching the schema in the system prompt.",
		HedgeLabel, payload,
	)
	resp, err := client.Chat(ctx, ai.ChatRequest{
		Model:     synthesizerModel,
		System:    system,
		Messages:  []ai.Message{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: prompt}}}},
		MaxTokens: synthesizerMaxTokens,
	})
	if err != nil {
		return Brief{}, resp.Usage, fmt.Errorf("revise: %w", err)
	}
	if resp.StopReason == "max_tokens" {
		return Brief{}, resp.Usage, fmt.Errorf("revise: reply truncated by max_tokens")
	}
	revised, err := parseBrief(resp.Text)
	if err != nil {
		return Brief{}, resp.Usage, fmt.Errorf("revise: %w", err)
	}
	return revised, resp.Usage, nil
}

// runReviseLoop drives up to budget revision passes while the editor still fails.
// Adopt-only-if-clean: it returns the first (brief, report) pair that re-edits
// clean, or the ORIGNAL pair if no revision does. It never errors: any failure
// inside the loop means the prior pair ships (fail-open, like delivery itself).
func runReviseLoop(ctx context.Context, chat ai.ChatClient, synthSystem, editorSystem string, plan Plan, reports []ResearchReport, brief Brief, report EditorReport, budget int, logger *log.Logger) (Brief, EditorReport, ai.Usage) {
	var total ai.Usage
	candidate, candReport := brief, report

	for attempt := 1; attempt <= budget && !candReport.Pass; attempt++ {
		revised, rusage, err := reviseBrief(ctx, chat, synthSystem, plan, reports, candidate, candReport.Failures)
		total.InputTokens += rusage.InputTokens
		total.OutputTokens += rusage.OutputTokens
		if err != nil {
			logger.Printf("revise attempt %d: %v; keeping prior brief", attempt, err)
			break
		}
		rep, eusage, err := editBrief(ctx, chat, editorSystem, revised, reports)
		total.InputTokens += eusage.InputTokens
		total.OutputTokens += eusage.OutputTokens
		if err != nil {
			logger.Printf("re-edit after revision %d: %v; keeping prior brief", attempt, err)
			break
		}

		candidate, candReport = revised, rep
		logger.Printf("revision %d: editor pass=%v", attempt, rep.Pass)
	}
	if !candReport.Pass {
		return brief, report, total
	}
	return candidate, candReport, total
}
