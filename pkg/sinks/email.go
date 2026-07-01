package sinks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultResendURL = "https://api.resend.com/emails"

// Email delivers a message as a plain-text email via the Resend API
// From/To/Subject are baked into sink (in destination config)
// sink interface stays a single Deliver method, BaseURL is overridable for tests
type Email struct {
	APIKey     string
	From       string
	To         []string
	Subject    string
	BaseURL    string
	HTTPClient *http.Client
}

func (e Email) Name() string { return "email" }

type resendEmail struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
}

func (e Email) Deliver(ctx context.Context, message string) error {
	if len(e.To) == 0 {
		return fmt.Errorf("email sink has no recipients")
	}

	base := e.BaseURL
	if base == "" {
		base = defaultResendURL
	}

	httpClient := e.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	body, err := json.Marshal(resendEmail{
		From:    e.From,
		To:      e.To,
		Subject: e.Subject,
		Text:    message,
	})
	if err != nil {
		return fmt.Errorf("marshal email: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build email request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+e.APIKey)

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("call resend: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read resend response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("resend %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	return nil
}
