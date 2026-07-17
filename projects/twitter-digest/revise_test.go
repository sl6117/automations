package twitterdigest

import (
	"context"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/ai"
)

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
