package weeklydeepdive

import (
	"io"
	"log"
	"testing"

	"github.com/sl6117/automations/internal/config"
	"github.com/sl6117/automations/internal/runner"
	"github.com/sl6117/automations/pkg/sinks"
)

func testRuntime(dryRun bool) *runner.Runtime {
	return &runner.Runtime{DryRun: dryRun, Log: log.New(io.Discard, "", 0)}
}

// the committed config.json must always parse and name at least one sink
func TestConfigJSONParses(t *testing.T) {
	var cfg Config
	if err := config.Load("config.json", &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.DeliverTo) == 0 {
		t.Fatal("config.json must name at least one sink")
	}
}

func TestMaxQuestionsDefaults(t *testing.T) {
	if got := (Config{}).maxQuestions(); got != maxResearchQuestions {
		t.Fatalf("default = %d, want %d", got, maxResearchQuestions)
	}
	if got := (Config{MaxResearchQuestions: 1}).maxQuestions(); got != 1 {
		t.Fatalf("override = %d, want 1", got)
	}
}

func TestSelectSinksDryRunForcesConsole(t *testing.T) {
	got, err := selectSinks(Config{DeliverTo: []string{"telegram", "console"}}, testRuntime(true))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 sink, got %d", len(got))
	}
	if _, ok := got[0].(sinks.Console); !ok {
		t.Fatalf("want console, got %T", got[0])
	}
}

func TestSelectSinksTelegramNeedsEnv(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_CHAT_ID", "")
	if _, err := selectSinks(Config{DeliverTo: []string{"telegram"}}, testRuntime(false)); err == nil {
		t.Fatal("want error when telegram env is missing")
	}
}

func TestSelectSinksTelegramFromEnv(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("TELEGRAM_CHAT_ID", "42")
	got, err := selectSinks(Config{DeliverTo: []string{"telegram"}}, testRuntime(false))
	if err != nil {
		t.Fatal(err)
	}
	tg, ok := got[0].(sinks.Telegram)
	if !ok {
		t.Fatalf("want telegram, got %T", got[0])
	}
	if tg.ChatID != "42" {
		t.Fatalf("chat id = %q, want 42", tg.ChatID)
	}
}
