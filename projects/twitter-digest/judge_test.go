package twitterdigest

import (
	"context"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/pkg/sources"
)

var judgeTweets = []sources.Tweet{
	{Author: "Dario Amodei", Handle: "@darioa", Text: "AI will be powerful", URL: "https://x.com/i/3"},
}
var judgeTopics = []Topic{{Name: "AI", Description: "models and agents"}, {Name: "Crypto"}}

const verdictJSON = `{"faithfulness":{"pass":true,"reason":""},"topicRouting":{"pass":true,"reason":""},"coverage":{"pass":false,"reason":"dropped the Dario tweet"},"clarity":{"pass":true,"reason":""}}`

func TestJudgePromptContainsGroundTruthAndDigest(t *testing.T) {
	got, err := buildJudgePrompt(".", judgeTopics, judgeTweets, "## AI\n- the digest body", "Korean")
	if err != nil {
		t.Fatalf("buildJudgePrompt error: %v", err)
	}
	for _, want := range []string{"AI will be powerful", "@darioa", "- AI: models and agents", "the digest body", "Korean"} {
		if !strings.Contains(got, want) {
			t.Errorf("judge prompt missing %q", want)
		}
	}
	if strings.Contains(got, "{{") {
		t.Errorf("unreplaced placeholder remains:\n%s", got)
	}
}
func TestJudgeParsesCleanVerdictsAndUsesJudgeSettings(t *testing.T) {
	fake := &fakeClient{resp: ai.Response{Text: verdictJSON, Usage: ai.Usage{InputTokens: 100, OutputTokens: 50}}}
	report, usage, err := judgeDigest(context.Background(), fake, "test-model", ".", judgeTopics, judgeTweets, "## AI\n- story", "English")
	if err != nil {
		t.Fatalf("judgeDigest error: %v", err)
	}
	if !report.Faithfulness.Pass || report.Coverage.Pass {
		t.Errorf("verdicts misread: %+v", report)
	}
	if report.Coverage.Reason != "dropped the Dario tweet" {
		t.Errorf("coverage reason misread: %q", report.Coverage.Reason)
	}
	if usage.InputTokens != 100 || usage.OutputTokens != 50 {
		t.Errorf("usage not passed through: %+v", usage)
	}
	if fake.gotReq.Temperature != judgeTemperature || fake.gotReq.MaxTokens != judgeMaxTokens {
		t.Errorf("judge sent digest settings, want temp=%v maxTokens=%v, got %+v", judgeTemperature, judgeMaxTokens, fake.gotReq)
	}
}
func TestJudgeToleratesFencedResponse(t *testing.T) {
	fenced := "Here is my evaluation:\n```json\n" + verdictJSON + "\n```\nHope that helps!"
	fake := &fakeClient{resp: ai.Response{Text: fenced}}
	report, _, err := judgeDigest(context.Background(), fake, "test-model", ".", judgeTopics, judgeTweets, "## AI\n- story", "English")
	if err != nil {
		t.Fatalf("judgeDigest should tolerate fences: %v", err)
	}
	if report.Coverage.Pass {
		t.Errorf("verdicts misread through fences: %+v", report)
	}
}
func TestJudgeRejectsMissingDimension(t *testing.T) {
	missing := `{"faithfulness":{"pass":true,"reason":""},"topicRouting":{"pass":true,"reason":""},"coverage":{"pass":true,"reason":""}}`
	fake := &fakeClient{resp: ai.Response{Text: missing}}
	if _, _, err := judgeDigest(context.Background(), fake, "test-model", ".", judgeTopics, judgeTweets, "## AI\n- story", "English"); err == nil {
		t.Fatal("a response missing a dimension must be an error, not a silent zero-value verdict")
	}
}
func TestJudgeReportsUsageEvenWhenUnparseable(t *testing.T) {
	fake := &fakeClient{resp: ai.Response{Text: "I cannot evaluate this.", Usage: ai.Usage{InputTokens: 80, OutputTokens: 10}}}
	_, usage, err := judgeDigest(context.Background(), fake, "test-model", ".", judgeTopics, judgeTweets, "## AI\n- story", "English")
	if err == nil {
		t.Fatal("garbage response must be an error")
	}
	if usage.InputTokens != 80 {
		t.Errorf("tokens were spent and must be reported even on parse failure: %+v", usage)
	}
}
