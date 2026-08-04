// pacakge ai is a generic, provider-agnostic LLM client
// knows nothing abt any project's domian: callers build the prompt
// ai sends it to a model and returns the completion plus token usage
package ai

import "context"

// StopMaxTokens is the normalized stop reason for a completion the model did not finish:
// it hit the caller's MaxTokens ceiling and was cut off mid-output. Providers spell this
// differently ("max_tokens", "length"); adapters translate to this one value.
const StopMaxTokens = "max_tokens"

// reports tokens consumed by a single completion (cost logging)
type Usage struct {
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int // written this call (~1.25x input price)
	CacheReadInputTokens     int // served from cache (~0.1x input price)
}

// TotalInputTokens is everything the model processed as input.
// With prompt caching, InputTokens alone is only the uncached suffix.
func (u Usage) TotalInputTokens() int {
	return u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
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
	Text       string
	Model      string
	StopReason string // why generation ended; StopMaxTokens means the text is cut off
	Usage      Usage
}

// Client is any LLM BE that can complete a request
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}
