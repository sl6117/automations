package ai

import "testing"

func TestUsageTotalInputTokens(t *testing.T) {
	// With prompt caching, Anthropic's input_tokens is only the uncached tail.
	// Total processed input is the sum of the three fields.
	u := Usage{
		InputTokens:              50,
		CacheCreationInputTokens: 4000,
		CacheReadInputTokens:     4100,
		OutputTokens:             20,
	}
	if got := u.TotalInputTokens(); got != 8150 {
		t.Errorf("TotalInputTokens() = %d, want 8150", got)
	}
	if (Usage{}).TotalInputTokens() != 0 {
		t.Error("zero Usage must total 0")
	}
}
