package twitterdigest

import (
	"context"
	"strings"
	"testing"

	"github.com/sl6117/automations/internal/storage"
)

func TestSplitSections(t *testing.T) {
	digest := "preamble ignored\n## AI\n- bullet one\n- bullet two\n\n## Stocks\n- bullet three"
	sections := splitSections(digest)

	if len(sections) != 2 {
		t.Fatalf("got %d sections, want 2: %+v", len(sections), sections)
	}
	if sections[0].Topic != "AI" || !strings.Contains(sections[0].Body, "bullet two") {
		t.Errorf("AI section wrong: %+v", sections[0])
	}
	if sections[1].Topic != "Stocks" || sections[1].Body != "- bullet three" {
		t.Errorf("Stocks section wrong: %+v", sections[1])
	}
}

func TestAssembleFor(t *testing.T) {
	sections := []Section{
		{Topic: "AI", Body: "- ai bullet"},
		{Topic: "Stocks", Body: "- stocks bullet"},
		{Topic: "Other", Body: "- misc bullet"},
	}

	t.Run("named topics get only those, no Other", func(t *testing.T) {
		got := assembleFor(Subscriber{Topics: []string{"stocks"}}, sections)
		if !strings.Contains(got, "stocks bullet") || strings.Contains(got, "ai bullet") || strings.Contains(got, "misc bullet") {
			t.Errorf("wrong sections for named subscriber:\n%s", got)
		}
	})

	t.Run("wildcard gets everything including Other", func(t *testing.T) {
		got := assembleFor(Subscriber{Topics: []string{"*"}}, sections)
		for _, want := range []string{"ai bullet", "stocks bullet", "misc bullet"} {
			if !strings.Contains(got, want) {
				t.Errorf("wildcard digest missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("no matching content means empty string", func(t *testing.T) {
		if got := assembleFor(Subscriber{Topics: []string{"Crypto"}}, sections); got != "" {
			t.Errorf("want empty digest, got:\n%s", got)
		}
	})
}

func TestLoadSubscribers(t *testing.T) {

	ctx := context.Background()
	t.Run("missing file is legacy mode, not an error", func(t *testing.T) {

		subs, err := loadSubscribers(ctx, &storage.FS{Root: t.TempDir()})
		if err != nil || subs != nil {
			t.Errorf("got (%v, %v), want (nil, nil)", subs, err)
		}
	})

	t.Run("valid file round-trips", func(t *testing.T) {
		store := &storage.FS{Root: t.TempDir()}

		content := `[{"name": "me", "sink": "telegram", "chatId": "123", "topics": ["*"]}]`

		if err := store.Put(ctx, subscribersKey, []byte(content)); err != nil {
			t.Fatal(err)
		}
		subs, err := loadSubscribers(ctx, store)
		if err != nil || len(subs) != 1 || subs[0].ChatID != "123" {
			t.Errorf("got (%+v, %v), want one subscriber with chatId 123", subs, err)
		}
	})
}
