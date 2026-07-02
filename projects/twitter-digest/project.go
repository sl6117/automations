package twitterdigest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/internal/config"
	"github.com/sl6117/automations/internal/obs"
	"github.com/sl6117/automations/internal/runner"
	"github.com/sl6117/automations/pkg/sinks"
	"github.com/sl6117/automations/pkg/sources"
)

const (
	digestTemperature = 0.2
	digestMaxTokens   = 1500
)

func init() {
	runner.Register(&project{})
}

type project struct {
	client ai.Client
	source sources.Source
	sinks  []sinks.Sink
}

func (p *project) Name() string { return "twitter-digest" }

func (p *project) Run(ctx context.Context, runTime *runner.Runtime) error {
	var cfg Config
	if err := config.Load(filepath.Join(runTime.ProjectDir, "config.json"), &cfg); err != nil {
		return err
	}
	// gather
	state, err := loadState()
	if err != nil {
		return err
	}
	source := p.source
	if source == nil {
		selected, err := selectSource(cfg, state.SinceID)
		if err != nil {
			return err
		}
		source = selected
	}
	tweets, err := source.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("fetch from %s: %w", source.Name(), err)
	}

	// process (no tokens) + reason (heuristic, no tokens)
	kept := filter(tweets, cfg.MinEngagement, cfg.MaxPerAuthor)

	if len(kept) == 0 {
		runTime.Log.Println("[twitter-digest] no tweets to digest - skipping send")

		if !runTime.DryRun {
			if err := advanceCursor(runTime, state, tweets); err != nil {
				return err
			}
		}
		return nil
	}

	runTime.Log.Printf("[twitter-digest] %d fetched -> %d kept", len(tweets), len(kept))

	message, usage, err := p.digest(ctx, runTime, cfg, kept)
	if err != nil {
		return err
	}

	if _, err := obs.LogRun(obs.Run{
		Project:      p.Name(),
		Model:        cfg.Model,
		DryRun:       runTime.DryRun,
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		ItemCount:    len(kept),
	}); err != nil {
		return fmt.Errorf("log run: %w", err)
	}

	if runTime.DryRun {
		runTime.Log.Println("[twitter-digest] dry-run: would have delivered:", message)
		return nil
	}

	deliverSinks := p.sinks
	if deliverSinks == nil {
		selected, err := selectSinks(cfg, runTime)
		if err != nil {
			return err
		}
		deliverSinks = selected
	}

	for _, sink := range deliverSinks {
		if err := sink.Deliver(ctx, message); err != nil {
			return fmt.Errorf("delivery via %s: %w", sink.Name(), err)
		}
	}
	return advanceCursor(runTime, state, tweets)
}

func (p *project) digest(ctx context.Context, runTime *runner.Runtime, cfg Config, kept []sources.Tweet) (string, ai.Usage, error) {

	client := p.client

	if client == nil {
		c, err := selectClient(cfg)
		if err != nil {
			return "", ai.Usage{}, err
		}
		client = c
	}

	if runTime.DryRun || client == nil {
		if !runTime.DryRun {
			runTime.Log.Println("[twitter-digest] no LLM API Key; using offline heuristic")
		}
		return render(summarize(kept, cfg.Topics)), ai.Usage{}, nil
	}
	prompt, err := buildPrompt(runTime.ProjectDir, cfg.Topics, kept)
	if err != nil {
		return "", ai.Usage{}, err
	}

	resp, err := client.Complete(ctx, ai.Request{
		Model:       cfg.Model,
		Prompt:      prompt,
		Temperature: digestTemperature,
		MaxTokens:   digestMaxTokens,
	})
	if err != nil {
		return "", ai.Usage{}, fmt.Errorf("summarize via %s: %w", cfg.Model, err)
	}
	runTime.Log.Printf("[twitter-digest] model=%s tokens in=%d out=%d", resp.Model, resp.Usage.InputTokens, resp.Usage.OutputTokens)
	return resp.Text, resp.Usage, nil
}

func selectSource(cfg Config, sinceID string) (sources.Source, error) {
	switch cfg.Source {
	case "", "mock":
		return sources.Mock{}, nil
	case "x", "xapi":
		token := os.Getenv("X_BEARER_TOKEN")
		if token == "" {
			return nil, fmt.Errorf("source %q needs X_BEARER_TOKEN in .env", cfg.Source)
		}
		listID := os.Getenv("X_LIST_ID")
		if listID == "" {
			listID = cfg.ListID
		}
		if listID == "" {
			return nil, fmt.Errorf("source %q needs a list id (config.json listId or X_LIST_ID)", cfg.Source)
		}
		return sources.XAPI{BearerToken: token, ListID: listID, SinceID: sinceID}, nil
	default:
		return nil, fmt.Errorf("unknown source: %q", cfg.Source)
	}
}

func selectClient(cfg Config) (ai.Client, error) {
	switch cfg.Provider {
	case "", "openrouter":
		key := os.Getenv("OPENROUTER_API_KEY")
		if key == "" {
			return nil, nil
		}
		return ai.OpenRouter{APIKey: key}, nil
	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, nil
		}
		return ai.Anthropic{APIKey: key}, nil
	default:
		return nil, fmt.Errorf("unknown provider: %q", cfg.Provider)
	}
}

func selectSinks(cfg Config, runTime *runner.Runtime) ([]sinks.Sink, error) {
	targets := cfg.DeliverTo

	if len(targets) == 0 {
		targets = []string{"console"}
	}

	var selected []sinks.Sink
	for _, target := range targets {
		switch target {
		case "console":
			selected = append(selected, sinks.Console{Out: runTime.Log.Writer()})
		case "telegram":
			token := os.Getenv("TELEGRAM_BOT_TOKEN")
			chatID := os.Getenv("TELEGRAM_CHAT_ID")
			if token == "" || chatID == "" {
				return nil, fmt.Errorf("telegram sink needs TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID in .env")
			}
			selected = append(selected, sinks.Telegram{BotToken: token, ChatID: chatID})
		case "email":
			key := os.Getenv("RESEND_API_KEY")
			if key == "" {
				return nil, fmt.Errorf("email sink needs RESEND_API_KEY in .env")
			}
			if len(cfg.EmailTo) == 0 {
				return nil, fmt.Errorf("email sink needs emailTo in config.json")
			}
			subject := cfg.EmailSubject
			if subject == "" {
				subject = "Daily X Digest"
			}
			selected = append(selected, sinks.Email{
				APIKey:  key,
				From:    cfg.EmailFrom,
				To:      cfg.EmailTo,
				Subject: subject,
			})
		default:
			return nil, fmt.Errorf("unknown sink: %q", target)
		}
	}
	return selected, nil
}

// advanceCursor persists the newest fetched ID s.t the next run only sees newer tweets.
// Called only after the run's real work succeeded - a failed runretries the same tweets.
func advanceCursor(runTime *runner.Runtime, state State, tweets []sources.Tweet) error {
	id := newestID(tweets)
	if id == "" || id == state.SinceID {
		return nil
	}
	state.SinceID = id
	if err := saveState(state); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	runTime.Log.Printf("[twitter-digest] advanced cursor to %s", id)
	return nil
}
