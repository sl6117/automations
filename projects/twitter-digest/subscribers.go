package twitterdigest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sl6117/automations/internal/storage"
)

const subscribersKey = "projects/twitter-digest/subscribers.json"

// Subscriber is one digest recipient: which sink to use, where to send, which topics they want
// ("*" -> all topics including other)
// loaded via storage.Store (gitignored file on FS be)
// - delivery address is personal data and never shared in the repo
type Subscriber struct {
	Name     string   `json:"name"`
	Sink     string   `json:"sink"` // "telegram", "email", or "console"
	ChatID   string   `json:"chatId,omitempty"`
	Email    string   `json:"email,omitempty"`
	Topics   []string `json:"topics"`
	Language string   `json:"language,omitempty"`
}

func (s Subscriber) language() string {
	if s.Language == "" {
		return "English"
	}
	return s.Language
}

// loadSubscribers returns nil (no error) when the file doesn't exist
// callers fall back to the legacy deliverTo config
func loadSubscribers(ctx context.Context, store storage.Store) ([]Subscriber, error) {
	data, err := store.Get(ctx, subscribersKey)
	if errors.Is(err, storage.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read subscribers: %w", err)
	}
	var subs []Subscriber
	if err := json.Unmarshal(data, &subs); err != nil {
		return nil, fmt.Errorf("unmarshal subscribers: %w", err)
	}
	return subs, nil
}

func (s Subscriber) wants(topic string) bool {
	for _, t := range s.Topics {
		if t == "*" || strings.EqualFold(t, topic) {
			return true
		}
	}
	return false
}

// assembleFor builds one subscriber's personalized digest from the parsed sections
// Returns "" when none of their topics have content today
// - the router skips delivery entirely rather than sending an empty shell
func assembleFor(sub Subscriber, sections []Section) string {
	var buf strings.Builder
	matched := false

	for _, sec := range sections {
		if !sub.wants(sec.Topic) {
			continue
		}
		matched = true
		fmt.Fprintf(&buf, "\n## %s\n%s\n", sec.Topic, sec.Body)
	}
	if !matched {
		return ""
	}
	return "Daily X Digest — " + time.Now().Format("2006-01-02") + "\n" + strings.TrimSpace(buf.String())
}
