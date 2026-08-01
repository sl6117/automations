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

// WebSearchToolType is Anthropic's basic server-side web search. TheAPI runs the search;
// the client must not tryto execute it locally.
const WebSearchToolType = "web_search_20250305"

// SeverTool is an Anthropic-hosted tool declared alongside client tools.
type ServerTool struct {
	Type    string // e.g. WebSearchToolType
	Name    string // e.g. "web_search"
	MaxUses int
}

// ContentBlock is one piece of a chat message. Type selects which fields are meaningful: "text" (TEXT), "tool_use" (ID, Name, Input)
// or "tool_result" (ToolUseID, Content, IsError).
// Unknown / server blocks (server_tool_use, web_search_tool_result) keep their wire JSON in Raw so multi-turn
// continuatins can echo encrypted_content back to Antrhopic.
type ContentBlock struct {
	Type      string
	Text      string
	ID        string
	Name      string
	Input     json.RawMessage
	ToolUseID string
	Content   string
	IsError   bool
	Raw       json.RawMessage // set for passthrough blocks; when non-nil, Chat marshals Raw as-is
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
	ServerTools []ServerTool
	MaxTokens   int
	Temperature float64
}

// ChatResponse is the model's reply. StopReason "tool_use" means the
// caller must execute the tool_use blocks and continue the conversation.
type ChatResponse struct {
	StopReason string
	Text       string
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
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

type anthropicWireTool struct {
	Type        string          `json:"type,omitempty"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	MaxUses     int             `json:"max_uses,omitempty"`
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
	Model      string            `json:"model"`
	StopReason string            `json:"stop_reason"`
	Content    []json.RawMessage `json:"content"`
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
			raw, err := contentBlockToWire(b)
			if err != nil {
				return ChatResponse{}, err
			}
			wm.Content = append(wm.Content, raw)
		}
		wire.Messages = append(wire.Messages, wm)
	}
	for _, t := range req.Tools {
		wire.Tools = append(wire.Tools, anthropicWireTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	for _, t := range req.ServerTools {
		wire.Tools = append(wire.Tools, anthropicWireTool{Type: t.Type, Name: t.Name, MaxUses: t.MaxUses})
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
	for _, raw := range parsed.Content {
		block, err := contentBlockFromWire(raw)
		if err != nil {
			return ChatResponse{}, err
		}
		out.Content = append(out.Content, block)

		if block.Type == "text" {
			if out.Text != "" {
				out.Text += "\n"
			}
			out.Text += block.Text
		}
	}
	return out, nil
}

func contentBlockToWire(b ContentBlock) (json.RawMessage, error) {
	if len(b.Raw) > 0 {
		return append(json.RawMessage(nil), b.Raw...), nil
	}
	wb := anthropicWireBlock{
		Type: b.Type, Text: b.Text, ID: b.ID, Name: b.Name, Input: b.Input,
		ToolUseID: b.ToolUseID, Content: b.Content, IsError: b.IsError,
	}
	raw, err := json.Marshal(wb)
	if err != nil {
		return nil, fmt.Errorf("marshal content block: %w", err)
	}
	return raw, nil
}
func contentBlockFromWire(raw json.RawMessage) (ContentBlock, error) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ContentBlock{}, fmt.Errorf("content block type: %w", err)
	}
	switch probe.Type {
	case "text", "tool_use", "tool_result":
		var wb anthropicWireBlock
		if err := json.Unmarshal(raw, &wb); err != nil {
			return ContentBlock{}, fmt.Errorf("content block: %w", err)
		}
		return ContentBlock{
			Type: wb.Type, Text: wb.Text, ID: wb.ID, Name: wb.Name, Input: wb.Input,
			ToolUseID: wb.ToolUseID, Content: wb.Content, IsError: wb.IsError,
		}, nil
	default:
		// server_tool_use, web_search_tool_result, citations, …
		return ContentBlock{Type: probe.Type, Raw: append(json.RawMessage(nil), raw...)}, nil
	}
}
