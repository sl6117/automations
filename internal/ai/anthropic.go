package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultAnthropicURL = "https://api.anthropic.com/v1/messages"
	anthropicVersion    = "2023-06-01"
)

// Anthropic is a Client backed by the Anthropic API
type Anthropic struct {
	APIKey     string
	BaseURL    string       // default is defaultAnthropicURL -> overrideable for testing
	HTTPClient *http.Client // defaults to a client with a sane timeout
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
}

type anthropicResponse struct {
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (a Anthropic) Complete(ctx context.Context, req Request) (Response, error) {
	url := a.BaseURL
	if url == "" {
		url = defaultAnthropicURL
	}

	httpClient := a.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	body, err := json.Marshal(anthropicRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		System:      req.System,
		Messages: []anthropicMessage{
			{Role: "user", Content: req.Prompt},
		},
	})

	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq.Header.Set("x-api-key", a.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("call anthropic: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("anthropic %d: %s", resp.StatusCode, truncate(string(data), 300))
	}

	var parsedResponse anthropicResponse
	if err := json.Unmarshal(data, &parsedResponse); err != nil {
		return Response{}, fmt.Errorf("unmarshal response: %w", err)
	}

	var text string
	for _, block := range parsedResponse.Content {
		if block.Type == "text" {
			text = block.Text
			break
		}
	}
	if text == "" {
		return Response{}, fmt.Errorf("no text in response")
	}

	return Response{
		Text:  text,
		Model: parsedResponse.Model,
		Usage: Usage{
			InputTokens:  parsedResponse.Usage.InputTokens,
			OutputTokens: parsedResponse.Usage.OutputTokens,
		},
	}, nil
}
