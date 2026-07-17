package twitterdigest

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/sl6117/automations/internal/ai"
	"github.com/sl6117/automations/internal/config"
	"github.com/sl6117/automations/internal/obs"
	"github.com/sl6117/automations/internal/storage"
)

const artifactPrefix = "logs/runs/"

// ReplayJudge runs the tier-2 judge over stored run artifacts that have no verdicts yet, including whose previous judge attempt errored
// (force re-judge all), writing reports back in place
// a nil client is resolved from config/env, same as a live run
func ReplayJudge(ctx context.Context, store storage.Store, client ai.Client, projectDir string, force bool, logger *log.Logger) error {
	var cfg Config
	if err := config.Load(filepath.Join(projectDir, "config.json"), &cfg); err != nil {
		return err
	}
	if client == nil {
		c, err := selectClient(cfg)
		if err != nil {
			return err
		}
		if c == nil {
			return fmt.Errorf("rejudge needs an LLM API key in .env")
		}
		client = c
	}
	keys, err := store.List(ctx, artifactPrefix)
	if err != nil {
		return fmt.Errorf("listing artifacts: %w", err)
	}

	var total ai.Usage
	judged, skipped := 0, 0

	for _, key := range keys {
		if !strings.Contains(key, "-twitter-digest-") {
			continue
		}
		data, err := store.Get(ctx, key)
		if err != nil {
			return fmt.Errorf("getting artifact %s: %w", key, err)
		}
		var a Artifact
		if err := json.Unmarshal(data, &a); err != nil {
			return fmt.Errorf("unmarshalling artifact %s: %w", key, err)
		}
		if !force && a.Judge != nil {
			skipped++
			continue
		}

		report, usage, jerr := judgeDigest(ctx, client, cfg.judgeModel(), projectDir, cfg.Topics, a.Kept, a.Digest, a.Language)
		total.InputTokens += usage.InputTokens
		total.OutputTokens += usage.OutputTokens
		if jerr != nil {
			a.Judge = nil
			a.JudgeError = jerr.Error()
			logger.Printf("[rejudge] %s: judge error: %v", key, jerr)
		} else {
			a.Judge = &report
			a.JudgeError = ""
			if fails := report.Failures(); len(fails) == 0 {
				logger.Printf("[rejudge] %s: passed", key)
			} else {
				for _, f := range fails {
					logger.Printf("[rejudge] %s: %s", key, f)
				}
			}
		}
		// put directly at original key: saveArtifact would mint a new timestamped key and re-stamp timestamp, splitting one run's record into 2
		updated, err := json.MarshalIndent(a, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal %s: %w", key, err)
		}
		if err := store.Put(ctx, key, updated); err != nil {
			return fmt.Errorf("put %s: %w", key, err)
		}
		judged++
	}
	logger.Printf("[rejudge] %d judged, %d skipped", judged, skipped)
	if judged > 0 {
		if _, err := obs.LogRun(ctx, store, obs.Run{
			Project:      "twitter-digest",
			Model:        cfg.judgeModel(),
			InputTokens:  total.InputTokens,
			OutputTokens: total.OutputTokens,
			ItemCount:    judged,
		}); err != nil {
			return fmt.Errorf("log run: %w", err)
		}
	}
	return nil
}
