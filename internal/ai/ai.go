// pacakge ai is a generic, provider-agnostic LLM client
// knows nothing abt any project's domian: callers build the prompt
// ai sends it to a model and returns the completion plus token usage
package ai

import "context"

// reports tokens consumed by a single completion (cost logging)
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Request is a single completion request
type Request struct {
	Model       string
	System      string // system prompt (optional)
	Prompt      string // user prompt
	MaxTokens   int
	Temperature float64
}

// Response is the model's reply plus accounting metadata.
type Response struct {
	Text  string
	Model string
	Usage Usage
}

// Client is any LLM BE that can complete a request
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}
