package twitterdigest

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/ai"
)

const failFaithJSON = `{"faithfulness":{"reason":"wrong date","pass":false},"topicRouting":{"reason":"","pass":true},"coverage":{"reason":"","pass":true},"clarity":{"reason":"","pass":true}}`
const passAllJSON = `{"faithfulness":{"reason":"","pass":true},"topicRouting":{"reason":"","pass":true},"coverage":{"reason":"","pass":true},"clarity":{"reason":"","pass":true}}`

// scriptedClient answers judge prompts from a fixed verdict list and revise prompts
// with canned text; an unexpected extra judge call panics the test loudly
type scriptedClient struct {
	verdicts    []string
	reviseText  string
	failRevise  bool
	judgeCalls  int
	reviseCalls int
}

func (m *scriptedClient) Complete(ctx context.Context, req ai.Request) (ai.Response, error) {
	if strings.Contains(req.Prompt, "quality evaluator") {
		v := m.verdicts[m.judgeCalls]
		m.judgeCalls++
		return ai.Response{Text: v, Usage: ai.Usage{InputTokens: 5, OutputTokens: 5}}, nil
	}
	m.reviseCalls++
	if m.failRevise {
		return ai.Response{}, fmt.Errorf("revise call exploded")
	}
	return ai.Response{Text: m.reviseText, Usage: ai.Usage{InputTokens: 10, OutputTokens: 10}}, nil
}

func failingReport() *JudgeReport {
	return &JudgeReport{
		Faithfulness: Verdict{Pass: false, Reason: "wrong date"},
		TopicRouting: Verdict{Pass: true}, Coverage: Verdict{Pass: true}, Clarity: Verdict{Pass: true},
	}
}

func TestReviseLoopOffWhenBudgetZero(t *testing.T) {
	fake := &scriptedClient{}
	logger := log.New(io.Discard, "", 0)
	draft, report, usage := runReviseLoop(context.Background(), fake, Config{Model: "m"}, ".", judgeTweets, "## AI\n- original", failingReport(), "English", logger)
	if draft != "## AI\n- original" || report.Faithfulness.Pass {
		t.Errorf("budget 0 must return the original pair untouched")
	}
	if fake.reviseCalls != 0 || fake.judgeCalls != 0 || usage.InputTokens != 0 {
		t.Errorf("budget 0 must make no LLM calls, got revise=%d judge=%d", fake.reviseCalls, fake.judgeCalls)
	}
}

func TestReviseLoopSkipsWhenFaithfulnessPasses(t *testing.T) {
	fake := &scriptedClient{}
	logger := log.New(io.Discard, "", 0)
	passing := &JudgeReport{Faithfulness: Verdict{Pass: true}}
	draft, _, _ := runReviseLoop(context.Background(), fake, Config{Model: "m", ReviseBudget: 2}, ".", judgeTweets, "## AI\n- original", passing, "English", logger)
	if draft != "## AI\n- original" || fake.reviseCalls != 0 {
		t.Errorf("passing faithfulness must not trigger revision")
	}
}

func TestReviseLoopAdoptsCleanRevision(t *testing.T) {
	fake := &scriptedClient{verdicts: []string{passAllJSON}, reviseText: "## AI\n- revised"}
	logger := log.New(io.Discard, "", 0)
	draft, report, usage := runReviseLoop(context.Background(), fake, Config{Model: "m", ReviseBudget: 1}, ".", judgeTweets, "## AI\n- original", failingReport(), "English", logger)
	if draft != "## AI\n- revised" || !report.Faithfulness.Pass {
		t.Errorf("clean revision must be adopted, got %q", draft)
	}
	if usage.InputTokens != 15 || usage.OutputTokens != 15 {
		t.Errorf("usage = %+v, want revise+rejudge summed (15/15)", usage)
	}
}

func TestReviseLoopKeepsOriginalWhenRevisionStillFails(t *testing.T) {
	fake := &scriptedClient{verdicts: []string{failFaithJSON}, reviseText: "## AI\n- revised"}
	logger := log.New(io.Discard, "", 0)
	draft, report, _ := runReviseLoop(context.Background(), fake, Config{Model: "m", ReviseBudget: 1}, ".", judgeTweets, "## AI\n- original", failingReport(), "English", logger)
	if draft != "## AI\n- original" || report.Faithfulness.Pass {
		t.Errorf("still-failing revision must not ship; want original draft and report back")
	}
}

func TestReviseLoopErrorKeepsOriginal(t *testing.T) {
	fake := &scriptedClient{failRevise: true}
	logger := log.New(io.Discard, "", 0)
	draft, _, _ := runReviseLoop(context.Background(), fake, Config{Model: "m", ReviseBudget: 1}, ".", judgeTweets, "## AI\n- original", failingReport(), "English", logger)
	if draft != "## AI\n- original" {
		t.Errorf("revise error must fall back to the original draft")
	}
	if fake.judgeCalls != 0 {
		t.Errorf("no re-judge after a failed revision, got %d", fake.judgeCalls)
	}
}

func TestRevisePromptContainsGrountTruthDigestAndCritique(t *testing.T) {
	got, err := buildRevisePrompt(".", judgeTopics, judgeTweets, "## AI\n- the digest body", "faithfulness: wrong date on the Dario story", "Korean")

	if err != nil {
		t.Fatalf("buildRevisePrompt error: %v", err)
	}
	for _, want := range []string{"AI will be powerful", "- AI: models and agents", "the digest body", "wrong date on the Dario story", "Korean"} {
		if !strings.Contains(got, want) {
			t.Errorf("revise prompt missing %q", want)
		}
	}
	if strings.Contains(got, "{{") {
		t.Errorf("unreplaced placeholder remains:\n%s", got)
	}
}

func TestReviseDigestReturnsRevisionAndUsage(t *testing.T) {
	fake := &fakeClient{resp: ai.Response{Text: "## AI\n- revised body", Usage: ai.Usage{InputTokens: 70, OutputTokens: 30}}}
	revised, usage, err := reviseDigest(context.Background(), fake, "test-model", ".", judgeTopics, judgeTweets, "## AI\n- story", "faithfulness: bad cite", "English")
	if err != nil {
		t.Fatalf("reviseDigest error: %v", err)
	}
	if revised != "## AI\n- revised body" {
		t.Errorf("revised = %q", revised)
	}
	if usage.InputTokens != 70 || usage.OutputTokens != 30 {
		t.Errorf("usage = %+v, want 70/30", usage)
	}
}
