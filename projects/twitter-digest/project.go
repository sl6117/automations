package twitterdigest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/internal/config"
	"github.com/sl6117/automations/internal/obs"
	"github.com/sl6117/automations/internal/queue"
	"github.com/sl6117/automations/internal/runner"
	"github.com/sl6117/automations/internal/storage"
	"github.com/sl6117/automations/pkg/sinks"
	"github.com/sl6117/automations/pkg/sources"
)

const (
	digestTemperature = 0.2
	digestMaxTokens   = 1500

	queueName           = "twitter-digest"
	deliveryLease       = 2 * time.Minute
	deliveryMaxAttempts = 5
)

func init() {
	runner.Register(&project{})
}

type project struct {
	client  ai.Client
	source  sources.Source
	sinks   []sinks.Sink
	sinkFor func(Subscriber, Config, *runner.Runtime) (sinks.Sink, error)
	store   storage.Store
	jobs    queue.Queue
	alert   sinks.Sink
}

func (p *project) Name() string { return "twitter-digest" }

func (p *project) Run(ctx context.Context, runTime *runner.Runtime) error {
	var cfg Config
	if err := config.Load(filepath.Join(runTime.ProjectDir, "config.json"), &cfg); err != nil {
		return err
	}

	store := p.store
	if store == nil {
		selected, err := storage.FromEnv(ctx)
		if err != nil {
			return err
		}
		store = selected
	}

	// gather
	state, err := loadState(ctx, store)
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

		if runTime.DryRun {
			return nil
		}

		if err := advanceCursor(ctx, store, runTime, state, tweets); err != nil {
			return err
		}

		// a quiet fetch still drains: leftover jobs from failed deliveries
		// must not wait for a day with new content
		subs, err := loadSubscribers(ctx, store)
		if err != nil {
			return err
		}
		if len(subs) == 0 {
			return nil
		}
		jobs, err := p.queueFromSeam(ctx)
		if err != nil {
			return err
		}
		return p.drain(ctx, runTime, cfg, subs, jobs)
	}

	runTime.Log.Printf("[twitter-digest] %d fetched -> %d kept", len(tweets), len(kept))

	subs, err := loadSubscribers(ctx, store)
	if err != nil {
		return err
	}

	languages := []string{"English"}

	if len(subs) > 0 {
		seen := map[string]bool{}
		languages = languages[:0]

		for _, sub := range subs {
			lang := sub.language()

			if !seen[lang] {
				seen[lang] = true
				languages = append(languages, lang)
			}
		}
	}

	digests := make(map[string]string, len(languages))
	var total ai.Usage

	for _, lang := range languages {
		message, usage, err := p.digest(ctx, runTime, cfg, kept, lang)
		if err != nil {
			return err
		}
		digests[lang] = message
		total.InputTokens += usage.InputTokens
		total.OutputTokens += usage.OutputTokens
		failures, coverage := evalDigest(message, kept, cfg.Topics)
		runTime.Log.Printf("[twitter-digest] eval (%s): %s", lang, coverage)
		for _, f := range failures {
			runTime.Log.Printf("[twitter-digest] eval failure (%s): %s", lang, f)
		}
		if !runTime.DryRun {
			if err := saveArtifact(ctx, store, Artifact{
				Model:        cfg.Model,
				Language:     lang,
				Kept:         kept,
				Digest:       message,
				InputTokens:  usage.InputTokens,
				OutputTokens: usage.OutputTokens,
				EvalFailures: failures,
				EvalCoverage: coverage,
			}); err != nil {
				return fmt.Errorf("save artifact: %w", err)
			}
		}
	}

	if _, err := obs.LogRun(context.Background(), store, obs.Run{
		Project:      p.Name(),
		Model:        cfg.Model,
		DryRun:       runTime.DryRun,
		InputTokens:  total.InputTokens,
		OutputTokens: total.OutputTokens,
		ItemCount:    len(kept),
	}); err != nil {
		return fmt.Errorf("log run: %w", err)
	}
	if runTime.DryRun {
		for _, lang := range languages {
			runTime.Log.Println("[twitter-digest] dry-run: would have delivered ("+lang+"):", digests[lang])
		}
		return nil
	}

	if len(subs) == 0 {
		// legacy mode: no subscribers.json, everyone gets everything
		deliverSinks := p.sinks
		if deliverSinks == nil {
			selected, err := selectSinks(cfg, runTime)
			if err != nil {
				return err
			}
			deliverSinks = selected
		}
		for _, sink := range deliverSinks {
			if err := sink.Deliver(ctx, digests["English"]); err != nil {
				return fmt.Errorf("delivery via %s: %w", sink.Name(), err)
			}
		}
		return advanceCursor(ctx, store, runTime, state, tweets)
	}

	jobs, err := p.queueFromSeam(ctx)
	if err != nil {
		return err
	}

	sections := make(map[string][]Section, len(digests))
	for lang, message := range digests {
		sections[lang] = splitSections(message)
	}

	// enqueue one durable job per subscriber. Once these are stored the content cannot be lost,
	// so the cursor advances immediately - delivery success no longer gates fetch progress
	for _, sub := range subs {
		personal := assembleFor(sub, sections[sub.language()])
		if personal == "" {
			runTime.Log.Printf("[twitter-digest] subscriber %s: no matching content", sub.Name)
			continue
		}
		id := newestID(tweets) + "#" + sub.Name
		if err := jobs.Enqueue(ctx, queueName, queue.Job{ID: id, Payload: []byte(personal)}); err != nil {
			return fmt.Errorf("enqueue job for %s: %w", sub.Name, err)
		}
	}
	if err := advanceCursor(ctx, store, runTime, state, tweets); err != nil {
		return err
	}
	return p.drain(ctx, runTime, cfg, subs, jobs)

}

func (p *project) queueFromSeam(ctx context.Context) (queue.Queue, error) {
	if p.jobs != nil {
		return p.jobs, nil
	}
	return queue.FromEnv(ctx)
}

// alertSink returns the operator-notification channel, best effort:
// nil means alerts are disabled and dead-letters only reach the log
func (p *project) alertSink() sinks.Sink {
	if p.alert != nil {
		return p.alert
	}
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	if token == "" || chatID == "" {
		return nil
	}
	return sinks.Telegram{BotToken: token, ChatID: chatID}
}

// drain claims and delivers every pending job, including leftovers from previous runs
// that's how a subscriber who failed yesterday gets yesterday's digest tody
// Failures settle the job (attempt counted, lease released)
// rather than failing the run: the queue owns retries now
func (p *project) drain(ctx context.Context, runTime *runner.Runtime, cfg Config, subs []Subscriber, jobs queue.Queue) error {
	sinkFor := p.sinkFor
	if sinkFor == nil {
		sinkFor = subscriberSink
	}

	byName := make(map[string]Subscriber, len(subs))
	for _, sub := range subs {
		byName[sub.Name] = sub
	}
	alert := p.alertSink()
	notify := func(msg string) {
		if alert == nil {
			return
		}
		if err := alert.Deliver(ctx, msg); err != nil {
			runTime.Log.Printf("[twitter-digest] alert delivery failed: %v", err)
		}
	}

	pending, err := jobs.Pending(ctx, queueName)
	if err != nil {
		return fmt.Errorf("pending jobs: %w", err)
	}

	for _, job := range pending {
		ok, err := jobs.Claim(ctx, queueName, job.ID, deliveryLease)

		if err != nil {
			return fmt.Errorf("claim %s: %w", job.ID, err)
		}
		if !ok {
			continue
		}

		_, name, found := strings.Cut(job.ID, "#")
		sub, known := byName[name]
		if !found || !known {
			if err := jobs.Fail(ctx, queueName, job.ID, fmt.Errorf("unknown subscriber %q", name), true); err != nil {
				return err
			}
			runTime.Log.Printf("[twitter-digest] job %s DEAD-LETTERED: unknown subscriber", job.ID)
			notify(fmt.Sprintf("twitter-digest: job %s dead-lettered: unknown subscriber", job.ID))
			continue
		}
		sink, err := sinkFor(sub, cfg, runTime)
		if err == nil {
			err = sink.Deliver(ctx, string(job.Payload))
		}
		if err != nil {
			final := job.Attempts+1 >= deliveryMaxAttempts
			if ferr := jobs.Fail(ctx, queueName, job.ID, err, final); ferr != nil {
				return ferr
			}
			if final {
				runTime.Log.Printf("[twitter-digest] job %s DEAD-LETTERED after %d attempts: %v", job.ID, job.Attempts+1, err)
				notify(fmt.Sprintf("twitter-digest: delivery to %s gave up after %d attempts: %v", sub.Name, job.Attempts+1, err))
			} else {
				runTime.Log.Printf("[twitter-digest] delivery FAILED (attempt %d/%d), queued for retry: %s: %v", job.Attempts+1, deliveryMaxAttempts, job.ID, err)
			}
			continue
		}
		if err := jobs.Complete(ctx, queueName, job.ID); err != nil {
			return err
		}
		runTime.Log.Printf("[twitter-digest] delivered to %s via %s", sub.Name, sub.Sink)
	}
	return nil
}

func (p *project) digest(ctx context.Context, runTime *runner.Runtime, cfg Config, kept []sources.Tweet, language string) (string, ai.Usage, error) {

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
	prompt, err := buildPrompt(runTime.ProjectDir, cfg.Topics, kept, language)
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

// subscriberSink builds the delivery sink for one subscriber.
// shared credentials (bot token, api key) come from .env; per person
// address comes from the subscriber object
func subscriberSink(sub Subscriber, cfg Config, runTime *runner.Runtime) (sinks.Sink, error) {
	switch sub.Sink {
	case "console":
		return sinks.Console{Out: runTime.Log.Writer()}, nil
	case "telegram":
		token := os.Getenv("TELEGRAM_BOT_TOKEN")
		if token == "" {
			return nil, fmt.Errorf("subscriber %s: telegram needs TELEGRAM_BOT_TOKEN in .env", sub.Name)
		}
		if sub.ChatID == "" {
			return nil, fmt.Errorf("subscriber %s: missing chatId", sub.Name)
		}
		return sinks.Telegram{BotToken: token, ChatID: sub.ChatID}, nil
	case "email":
		key := os.Getenv("RESEND_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("subscriber %s: email needs RESEND_API_KEY in .env", sub.Name)
		}
		if sub.Email == "" {
			return nil, fmt.Errorf("subscriber %s: missing email", sub.Name)
		}
		subject := cfg.EmailSubject
		if subject == "" {
			subject = "Daily X Digest"
		}
		return sinks.Email{APIKey: key, From: cfg.EmailFrom, To: []string{sub.Email}, Subject: subject}, nil
	default:
		return nil, fmt.Errorf("subscriber %s: unknown sink %q", sub.Name, sub.Sink)
	}
}

// advanceCursor persists the newest fetched ID s.t the next run only sees newer tweets.
// Called only after the run's real work succeeded - a failed runretries the same tweets.
func advanceCursor(ctx context.Context, store storage.Store, runTime *runner.Runtime, state State, tweets []sources.Tweet) error {
	id := newestID(tweets)
	if id == "" || id == state.SinceID {
		return nil
	}
	state.SinceID = id
	if err := saveState(ctx, store, state); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	runTime.Log.Printf("[twitter-digest] advanced cursor to %s", id)
	return nil
}
