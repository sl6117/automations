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

const defaultJevURL = "https://api.typesafe.ai/v1/systemone"
const defaultJevModel = "jev-1.13.0"

// Jev calls TypeSafe's decision API. It returns yes/no probabilities and does not generate text.
type Jev struct {
	APIKey     string
	BaseURL    string // empty uses defaultJevURL; tests point this at an httptest server
	Model      string // empty uses defaultJevModel
	HTTPClient *http.Client
}

// jevQuestion is one typed question. Type is always "noul" for a yes/no probability.
type jevQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
}

// jevRequest is the POST body: the text under judgment, and the questions to ask about it.
type jevRequest struct {
	Model     string                 `json:"model"`
	State     string                 `json:"state"`
	Questions map[string]jevQuestion `json:"questions"`
}

// jevNoul is one answer. Noul is P(yes), from 0 to 1.
type jevNoul struct {
	Noul float64 `json:"noul"`
}

type jevResponse struct {
	Model   string             `json:"model"`
	Answers map[string]jevNoul `json:"answers"`
}

// Nouls asks each question as a yes/no and returns P(yes) under the same name.
func (j Jev) Nouls(ctx context.Context, state string, questions map[string]string) (map[string]float64, error) {
	url := j.BaseURL
	if url == "" {
		url = defaultJevURL
	}
	httpClient := j.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	asked := make(map[string]jevQuestion, len(questions))
	for name, instructions := range questions {
		asked[name] = jevQuestion{Type: "noul", Instructions: instructions}
	}
	model := j.Model
	if model == "" {
		model = defaultJevModel
	}
	body, err := json.Marshal(jevRequest{Model: model, State: state, Questions: asked})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+j.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call jev: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jev %d: %s", resp.StatusCode, truncate(string(data), 300))
	}

	var parsed jevResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	out := make(map[string]float64, len(questions))
	for name := range questions {
		answer, ok := parsed.Answers[name]
		if !ok {
			return nil, fmt.Errorf("jev response missing %q", name)
		}
		out[name] = answer.Noul
	}
	return out, nil
}
