package sinks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultTelegramBaseURL = "https://api.telegram.org"
	telegramChunkLimit     = 4000 // under 4096 telegram cap
)

// Telegram delivers a message via the Bot API sendMessage method
// BaseURL is overridable so tests can point at httptest

type Telegram struct {
	BotToken   string
	ChatID     string
	BaseURL    string
	HTTPClient *http.Client
}

func (t Telegram) Name() string { return "telegram" }

type telegramSendMessage struct {
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

func (t Telegram) Deliver(ctx context.Context, message string) error {
	base := t.BaseURL
	if base == "" {
		base = defaultTelegramBaseURL
	}
	httpClient := t.HTTPClient

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", base, t.BotToken)

	for _, chunk := range splitMessage(message, telegramChunkLimit) {
		body, err := json.Marshal(telegramSendMessage{
			ChatID:                t.ChatID,
			Text:                  chunk,
			DisableWebPagePreview: true,
		})
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("build telegram request: %w", err)
		}

		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(httpReq)
		if err != nil {
			return fmt.Errorf("call telegram: %w", err)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			return fmt.Errorf("read response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("telegram %d: %s", resp.StatusCode, truncate(string(data), 300))
		}
	}
	return nil
}

// splitMessage breaks text into <=limit chunks on line boundaries, so a long digest survives Telegram's per-message cap
// a single line longer than limit is sent as-is (acceptable: digest lines are tweet-sized)
func splitMessage(text string, limit int) []string {
	if len(text) <= limit {
		return []string{text}
	}

	var parts []string
	var buf string

	for _, line := range strings.Split(text, "\n") {
		candidate := line

		if buf != "" {
			candidate = buf + "\n" + line
		}
		if len(candidate) > limit {
			if buf != "" {
				parts = append(parts, buf)
			}
			buf = line
		} else {
			buf = candidate
		}
	}

	if buf != "" {
		parts = append(parts, buf)
	}
	return parts
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
