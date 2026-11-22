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

	system, user, err := buildPrompt(".", []Topic{{Name: "AI", Description: "models and agents"}, {Name: "Crypto"}}, tweets, "English")
	if err != nil {
		t.Fatalf("buildPrompt error: %v", err)
	}
	for _, want := range []string{"Dario Amodei", "@darioa", "AI will be powerful", "- AI", "- Crypto"} {
		if !strings.Contains(system, want) {
			t.Errorf("system missing %q\n---\n%s", want, system)
		}
	}
	if strings.Contains(system, "{{") || strings.Contains(user, "{{") {
		t.Errorf("unreplaced placeholder remains:\nsystem=%s\nuser=%s", system, user)
	}
	// Language belongs in the user message so the system prefix is identical
	// across EN/KO/RU and can be prompt-cached within one run.
	if strings.Contains(system, "Write every summary in English") {
		t.Error("language directive must not be in system (breaks cross-language cache)")
	}
	if !strings.Contains(user, "English") {
		t.Errorf("user missing language:\n%s", user)
	}
	if strings.Contains(user, "Dario Amodei") {
		t.Error("tweets must stay in system (the stable cached prefix), not user")
	}
}

func TestBuildPromptSystemStableAcrossLanguages(t *testing.T) {
	tweets := []sources.Tweet{
		{Author: "A", Handle: "a", Text: "hello", URL: "https://x.com/a/status/1"},
	}
	topics := []Topic{{Name: "AI"}}
	sysEN, userEN, err := buildPrompt(".", topics, tweets, "English")
	if err != nil {
		t.Fatal(err)
	}
	sysKO, userKO, err := buildPrompt(".", topics, tweets, "Korean")
	if err != nil {
		t.Fatal(err)
	}
	if sysEN != sysKO {
		t.Error("system prefix must be byte-identical across languages for cache hits")
	}
	if userEN == userKO {
		t.Error("user message must differ by language")
	}
}

// The model-facing view is a contract shared by the digest prompt, the judge and Jev:
// the adapter's structured fields must never leak into it, or every prompt changes.
func TestSlimTweetsIgnoresStructuredFields(t *testing.T) {
	tweets := []sources.Tweet{{
		ID: "1900000000000000001", Author: "Example Wire", Handle: "@example_wire",
		Text:     "Not convinced\n[quoting @example_desk: $EXMPL guidance raised]",
		URL:      "https://x.com/example_wire/status/1900000000000000001",
		AuthorID: "1900000000000000901",
		OwnText:  "Not convinced",
		Refs: []sources.Ref{{Type: "quoted", ID: "1900000000000000012", AuthorID: "1900000000000000902",
			AuthorHandle: "@example_desk", Text: "$EXMPL guidance raised"}},
	}}
	got, err := slimTweets(tweets)
	if err != nil {
		t.Fatalf("slimTweets error: %v", err)
	}
	want := `[
  {
    "author": "Example Wire",
    "handle": "@example_wire",
    "url": "https://x.com/example_wire/status/1900000000000000001",
    "text": "Not convinced\n[quoting @example_desk: $EXMPL guidance raised]"
  }
]`
	if string(got) != want {
		t.Errorf("slimTweets =\n%s\nwant\n%s", got, want)
	}
}
