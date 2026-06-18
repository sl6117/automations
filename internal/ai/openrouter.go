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

const defaultOpenRouterURL = "https://openrouter.ai/api/v1/chat/completions"

// OpenRouter is a Client backed by the OpenRouter chat-completions API
type OpenRouter struct {
	APIKey     string
	BaseURL    string       // defualt is defaultOpenRouterURL -> overrideable for testing
	HTTPClient *http.Client // defaults to a client with a sane timeout
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n]
}

func (o OpenRouter) Complete(ctx context.Context, req Request) (Response, error) {
	url := o.BaseURL

	if url == "" {
		url = defaultOpenRouterURL
	}

	// setting up the HTTP client and sane timeout
	httpClient := o.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	messages := make([]chatMessage, 0, 2)

	if req.System != "" {
		messages = append(messages, chatMessage{Role: "system", Content: req.System})
	}
	messages = append(messages, chatMessage{Role: "user", Content: req.Prompt})

	body, err := json.Marshal(chatRequest{
		Model:       req.Model,
		Messages:    messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	})
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+o.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Title", "twitter-digest")

	resp, err := httpClient.Do(httpReq)

	if err != nil {
		return Response{}, fmt.Errorf("call to openrouter: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)

	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("openrouter %d: %s", resp.StatusCode, truncate(string(data), 300))
	}

	var chatResponseObj chatResponse

	if err := json.Unmarshal(data, &chatResponseObj); err != nil {
		return Response{}, fmt.Errorf("unmarshal response failed: %w", err)
	}
	if len(chatResponseObj.Choices) == 0 {
		return Response{}, fmt.Errorf("no choices in response")
	}

	return Response{
		Text:  chatResponseObj.Choices[0].Message.Content,
		Model: req.Model,
		Usage: Usage{
			InputTokens:  chatResponseObj.Usage.PromptTokens,
			OutputTokens: chatResponseObj.Usage.CompletionTokens,
		},
	}, nil

}
