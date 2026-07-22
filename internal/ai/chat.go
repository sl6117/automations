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

// ToolDef describes one callable tool. InputSchema is JSON Schema passed through verbatim
// - the MCP server already produced it; ai doesn't interpret it.
type ToolDef struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// ContentBlock is one piece of a chat message. Type selects which fields are meaningful: "text" (TEXT), "tool_use" (ID, Name, Input)
// or "tool_result" (ToolUseID, Content, IsError).
type ContentBlock struct {
	Type      string
	Text      string
	ID        string
	Name      string
	Input     json.RawMessage
	ToolUseID string
	Content   string
	IsError   bool
}

// Message is one turn in a conversation. Role is "user" or "assistant".
type Message struct {
	Role    string
	Content []ContentBlock
}

// ChatRequest is a multi-turn completion request with optional tools.
type ChatRequest struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []ToolDef
	MaxTokens   int
	Temperature float64
}

// ChatResponse is the model's reply. StopReason "tool_use" means the
// caller must execute the tool_use blocks and continue the conversation.
type ChatResponse struct {
	StopReason string
	Content    []ContentBlock
	Model      string
	Usage      Usage
}

// ChatClient is any LLM backend that supports tool-using converations.
type ChatClient interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

// wire types: one block struct with omitempty covers all three block shapes
// mirroring how the Anthropic API discriminates on "type"
type anthropicWireBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type anthropicWireMessage struct {
	Role    string               `json:"role"`
	Content []anthropicWireBlock `json:"content"`
}

type anthropicWireTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}
type anthropicChatRequest struct {
	Model       string                 `json:"model"`
	MaxTokens   int                    `json:"max_tokens"`
	Temperature float64                `json:"temperature"`
	System      string                 `json:"system,omitempty"`
	Messages    []anthropicWireMessage `json:"messages"`
	Tools       []anthropicWireTool    `json:"tools,omitempty"`
}
type anthropicChatResponse struct {
	Model      string               `json:"model"`
	StopReason string               `json:"stop_reason"`
	Content    []anthropicWireBlock `json:"content"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Chat sends a tool-capable conversation to the Anthropic messages API.
func (a Anthropic) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	url := a.BaseURL
	if url == "" {
		url = defaultAnthropicURL
	}
	httpClient := a.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	wire := anthropicChatRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		System:      req.System,
	}
	for _, m := range req.Messages {
		wm := anthropicWireMessage{Role: m.Role}
		for _, b := range m.Content {
			wm.Content = append(wm.Content, anthropicWireBlock(b))
		}
		wire.Messages = append(wire.Messages, wm)
	}
	for _, t := range req.Tools {
		wire.Tools = append(wire.Tools, anthropicWireTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("marshal chat request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("build chat request: %w", err)
	}
	httpReq.Header.Set("x-api-key", a.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("call anthropic: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, fmt.Errorf("anthropic %d: %s", resp.StatusCode, truncate(string(data), 300))
	}

	var parsed anthropicChatResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return ChatResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}
	out := ChatResponse{
		StopReason: parsed.StopReason,
		Model:      parsed.Model,
		Usage:      Usage{InputTokens: parsed.Usage.InputTokens, OutputTokens: parsed.Usage.OutputTokens},
	}
	for _, b := range parsed.Content {
		out.Content = append(out.Content, ContentBlock(b))
	}
	return out, nil
}
