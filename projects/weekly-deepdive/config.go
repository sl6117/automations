package weeklydeepdive

import (
	"fmt"
	"os"

	"github.com/sl6117/automations/internal/runner"
	"github.com/sl6117/automations/pkg/sinks"
)

// Config holds the non-secret knobs for weekly-deepdive (config.json).
type Config struct {
	DeliverTo            []string `json:"deliverTo"`
	MaxResearchQuestions int      `json:"maxResearchQuestions"`
}

// maxQuestions is the research fan-out cap; absent/0 falls back to the default cost guard.
func (c Config) maxQuestions() int {
	if c.MaxResearchQuestions > 0 {
		return c.MaxResearchQuestions
	}
	return maxResearchQuestions
}

// selectSinks builds delivery sinks from config.
// Dry runs always go to console only: --dry-run must never send to Telegram.
func selectSinks(cfg Config, rt *runner.Runtime) ([]sinks.Sink, error) {
	if rt.DryRun {
		return []sinks.Sink{sinks.Console{Out: rt.Log.Writer()}}, nil
	}
	targets := cfg.DeliverTo
	if len(targets) == 0 {
		targets = []string{"console"}
	}
	var selected []sinks.Sink
	for _, target := range targets {
		switch target {
		case "console":
			selected = append(selected, sinks.Console{Out: rt.Log.Writer()})
		case "telegram":
			token := os.Getenv("TELEGRAM_BOT_TOKEN")
			chatID := os.Getenv("TELEGRAM_CHAT_ID")
			if token == "" || chatID == "" {
				return nil, fmt.Errorf("telegram sink needs TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID in .env")
			}
			selected = append(selected, sinks.Telegram{BotToken: token, ChatID: chatID})
		default:
			return nil, fmt.Errorf("unknown sink: %q", target)
		}
	}
	return selected, nil
}
