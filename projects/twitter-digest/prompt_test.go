package twitterdigest

import (
	"strings"
	"testing"

	"github.com/sl6117/automations/pkg/sources"
)

func TestBuildPrompt(t *testing.T) {
	tweets := []sources.Tweet{
		{Author: "Dario Amodei", Handle: "@darioa", Text: "AI will be powerful", URL: "https://x.com/i/3"},
	}

	got, err := buildPrompt(".", []Topic{{Name: "AI", Description: "models and agents"}, {Name: "Crypto"}}, tweets)
	if err != nil {
		t.Fatalf("buildPrompt error: %v", err)
	}
	for _, want := range []string{"Dario Amodei", "@darioa", "AI will be powerful", "- AI", "- Crypto"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, "{{") {
		t.Errorf("unreplaced placeholder remains:\n%s", got)
	}
}
