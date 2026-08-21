package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	claudeAPIURL      = "https://api.anthropic.com/v1/messages"
	claudeAPIVersion  = "2023-06-01"
	claudeDefaultModel = "claude-sonnet-4-6"
	claudeMaxTokens   = 4096
)

// ClaudeAdapter calls the Anthropic Messages API.
type ClaudeAdapter struct {
	apiKey string
	model  string
	http   *http.Client
}

// NewClaudeAdapter creates an adapter for the Anthropic API.
// model defaults to claude-sonnet-4-6 if empty.
func NewClaudeAdapter(apiKey, model string) *ClaudeAdapter {
	if model == "" {
		model = claudeDefaultModel
	}
	return &ClaudeAdapter{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Timeout: 120 * time.Second},
	}
}

// Name returns the adapter identifier.
func (a *ClaudeAdapter) Name() string {
	return fmt.Sprintf("claude/%s", a.model)
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	Messages  []claudeMessage `json:"messages"`
}

type claudeResponse struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// Generate sends a prompt to the Anthropic API and returns the response.
func (a *ClaudeAdapter) Generate(ctx context.Context, prompt string) (Response, error) {
	reqBody := claudeRequest{
		Model:     a.model,
		MaxTokens: claudeMaxTokens,
		Messages:  []claudeMessage{{Role: "user", Content: prompt}},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return Response{}, fmt.Errorf("claude marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claudeAPIURL, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("claude request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", claudeAPIVersion)

	resp, err := a.http.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("claude http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("claude api: status %d", resp.StatusCode)
	}

	var cr claudeResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return Response{}, fmt.Errorf("claude decode: %w", err)
	}

	text := ""
	for _, block := range cr.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}

	return Response{
		Text:         text,
		PromptTokens: cr.Usage.InputTokens,
		TotalTokens:  cr.Usage.InputTokens + cr.Usage.OutputTokens,
		Model:        cr.Model,
	}, nil
}
