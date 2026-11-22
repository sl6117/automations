// Package knowledgefactory seals each day's desk feed from the corpus and, when enabled,
// publishes the sealed files to a private GitHub repo for the desk to read. It runs
// after the digest and only reads what the digest stored; it never calls X or a model.
package knowledgefactory

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sl6117/automations/internal/config"
	"github.com/sl6117/automations/internal/corpus"
	"github.com/sl6117/automations/internal/kf"
	"github.com/sl6117/automations/internal/obs"
	"github.com/sl6117/automations/internal/runner"
	"github.com/sl6117/automations/internal/storage"
	"github.com/sl6117/automations/pkg/sinks"
)

const (
	projectName = "knowledge-factory"

	// catchUpDays is how far back an unsealed day is still sealed and published. A day
	// that already has a sealed revision is never re-exported here: a universe edit would
	// otherwise mint a new revision of every past day on every run.
	catchUpDays = 3

	tokenEnv          = "KF_GITHUB_TOKEN"
	defaultGitHubAPI  = "https://api.github.com"
	githubHTTPTimeout = 30 * time.Second
)

func init() {
	runner.Register(&project{})
}

type Config struct {
	Publish PublishConfig `json:"publish"`
}

// PublishConfig gates delivery to GitHub. Off by default: turning it on is the owner's
// step after the repo, token and deploy exist.
type PublishConfig struct {
	Enabled    bool   `json:"enabled"`
	Repo       string `json:"repo"`       // owner/name
	Branch     string `json:"branch"`     // branch the files are committed to
	PathPrefix string `json:"pathPrefix"` // directory in the repo, e.g. desk-feed/v1
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

func (c PublishConfig) validate() error {
	if !repoPattern.MatchString(c.Repo) {
		return fmt.Errorf("publish.repo %q must be owner/name", c.Repo)
	}
	if c.Branch == "" {
		return errors.New("publish.branch is empty")
	}
	if c.PathPrefix == "" || strings.HasPrefix(c.PathPrefix, "/") || strings.HasSuffix(c.PathPrefix, "/") {
		return fmt.Errorf("publish.pathPrefix %q must be a relative path without leading or trailing slash", c.PathPrefix)
	}
	return nil
}

type project struct {
	store     storage.Store // main store, for the cost log
	corpus    storage.Store
	now       func() time.Time
	client    *http.Client
	githubAPI string
	alert     sinks.Sink
	getenv    func(string) string
}

func (p *project) Name() string { return projectName }

func (p *project) env(key string) string {
	if p.getenv != nil {
		return p.getenv(key)
	}
	return os.Getenv(key)
}

func (p *project) clock() func() time.Time {
	if p.now != nil {
		return p.now
	}
	return time.Now
}

// alertSink mirrors the digest's operator channel: the same Telegram env vars, and nil
// (log only) when they are unset.
func (p *project) alertSink() sinks.Sink {
	if p.alert != nil {
		return p.alert
	}
	token, chatID := p.env("TELEGRAM_BOT_TOKEN"), p.env("TELEGRAM_CHAT_ID")
	if token == "" || chatID == "" {
		return nil
	}
	return sinks.Telegram{BotToken: token, ChatID: chatID}
}

func (p *project) Run(ctx context.Context, rt *runner.Runtime) error {
	var cfg Config
	if err := config.Load(filepath.Join(rt.ProjectDir, "config.json"), &cfg); err != nil {
		return err
	}
	u, err := kf.LoadUniverse(filepath.Join(rt.ProjectDir, "universe.json"))
	if err != nil {
		return err
	}
	corpusStore := p.corpus
	if corpusStore == nil {
		if corpusStore, err = corpus.StoreFromEnv(ctx); err != nil {
			return fmt.Errorf("corpus store: %w", err)
		}
	}
	now := p.clock()
	today := now().UTC()

	if rt.DryRun {
		res, err := kf.Preview(ctx, corpusStore, u, today, now)
		switch {
		case errors.Is(err, kf.ErrNothingFetched):
			rt.Log.Printf("[%s] dry-run: %v", projectName, err)
		case err != nil:
			return err
		default:
			rt.Log.Printf("[%s] dry-run: %s would seal mentions %d, entity_day %d, posts_fetched %d; nothing written, nothing published",
				projectName, res.Date, res.Mentions, res.EntityDays, res.PostsFetched)
		}
		return nil
	}

	mainStore := p.store
	if mainStore == nil {
		if mainStore, err = storage.FromEnv(ctx); err != nil {
			return err
		}
	}

	mentions := 0
	res, err := kf.Export(ctx, corpusStore, u, today, now)
	switch {
	case errors.Is(err, kf.ErrNothingFetched):
		// the digest may not have run yet, or the flag is off: not a failure
		rt.Log.Printf("[%s] %v; skipping today", projectName, err)
	case err != nil:
		return fmt.Errorf("seal %s: %w", res.Date, err)
	default:
		logResult(rt, res)
		mentions = res.Mentions
	}

	for i := 1; i <= catchUpDays; i++ {
		day := today.AddDate(0, 0, -i)
		rev, err := kf.Sealed(ctx, corpusStore, day)
		if err != nil {
			return err
		}
		if rev > 0 {
			continue
		}
		res, err := kf.Export(ctx, corpusStore, u, day, now)
		if errors.Is(err, kf.ErrNothingFetched) {
			continue
		}
		if err != nil {
			return fmt.Errorf("catch-up seal %s: %w", res.Date, err)
		}
		rt.Log.Printf("[%s] catch-up:", projectName)
		logResult(rt, res)
	}

	if _, err := obs.LogRun(ctx, mainStore, obs.Run{Project: projectName, ItemCount: mentions}); err != nil {
		return fmt.Errorf("log run: %w", err)
	}

	if !cfg.Publish.Enabled {
		return nil
	}
	token := p.env(tokenEnv)
	err = p.publishRecent(ctx, rt, cfg.Publish, token, corpusStore, today)
	if err == nil {
		return nil
	}
	// Sealing already happened and is durable; the next run's catch-up publishes what
	// this one missed. The token never reaches the log, the alert or the returned error.
	msg := redact(fmt.Sprintf("%s: publishing to GitHub failed (sealed files are kept; the next run retries): %v", projectName, err), token)
	rt.Log.Println("[" + projectName + "] " + msg)
	if alert := p.alertSink(); alert != nil {
		if aerr := alert.Deliver(ctx, msg); aerr != nil {
			rt.Log.Printf("[%s] alert delivery failed: %s", projectName, redact(aerr.Error(), token))
		}
	}
	return errors.New(msg)
}

func logResult(rt *runner.Runtime, res kf.ExportResult) {
	outcome := fmt.Sprintf("%s r%d", res.Outcome, res.Revision)
	if res.Supersedes > 0 {
		outcome += fmt.Sprintf(" supersedes r%d", res.Supersedes)
	}
	rt.Log.Printf("[%s] %s: %s (mentions %d, entity_day %d, posts_fetched %d)",
		projectName, res.Date, outcome, res.Mentions, res.EntityDays, res.PostsFetched)
}

// publishRecent publishes every sealed revision of today and the catch-up days that the
// repo does not have yet, oldest revision first.
func (p *project) publishRecent(ctx context.Context, rt *runner.Runtime, cfg PublishConfig, token string, store storage.Store, today time.Time) error {
	if token == "" {
		return fmt.Errorf("publish.enabled is true but %s is not set", tokenEnv)
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	client := p.client
	if client == nil {
		client = &http.Client{Timeout: githubHTTPTimeout}
	}
	base := p.githubAPI
	if base == "" {
		base = defaultGitHubAPI
	}
	pub := &publisher{client: client, base: base, token: token, cfg: cfg, log: rt.Log}

	for i := catchUpDays; i >= 0; i-- {
		day := today.AddDate(0, 0, -i)
		latest, err := kf.Sealed(ctx, store, day)
		if err != nil {
			return err
		}
		for rev := 1; rev <= latest; rev++ {
			files, err := kf.RevisionFiles(ctx, store, day, rev)
			if err != nil {
				return err
			}
			if err := pub.publishRevision(ctx, day.Format("2006-01-02"), rev, files); err != nil {
				return err
			}
		}
	}
	return nil
}

// redact strips the token from s, as pkg/sinks/telegram.go does for its bot token.
func redact(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, "***REDACTED***")
}
