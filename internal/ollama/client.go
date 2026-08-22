package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	endpoint string
	http     *http.Client
}

func New(endpoint string, timeoutSec int) *Client {
	return &Client{
		endpoint: endpoint,
		http:     &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
	}
}

type GenerateOptions struct {
	NumPredict int `json:"num_predict,omitempty"`
}

type GenerateRequest struct {
	Model   string          `json:"model"`
	Prompt  string          `json:"prompt"`
	Stream  bool            `json:"stream"`
	Options *GenerateOptions `json:"options,omitempty"`
}

type GenerateResponse struct {
	Model              string `json:"model"`
	Response           string `json:"response"`
	Done               bool   `json:"done"`
	PromptEvalCount    int    `json:"prompt_eval_count"`
	EvalCount          int    `json:"eval_count"`
	TotalDurationNs    int64  `json:"total_duration"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type ChatResponse struct {
	Model   string      `json:"model"`
	Message ChatMessage `json:"message"`
	Done    bool        `json:"done"`
}

// isTransient returns true for connection-level errors (reset, refused, EOF)
// that are worth retrying after Ollama restarts.
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "connection refused") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "EOF") ||
		strings.Contains(s, "read tcp") ||
		strings.Contains(s, "empty response")
}

func (c *Client) GenerateFull(ctx context.Context, model, prompt string) (*GenerateResponse, error) {
	const maxRetries = 3
	wait := 5 * time.Second
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("ollama: retry %d/%d after %v (err: %v)", attempt, maxRetries-1, wait, lastErr)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			wait *= 2
		}
		result, err := c.generateOnce(ctx, model, prompt)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !isTransient(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("ollama generate: %w (after %d retries)", errors.Unwrap(lastErr), maxRetries)
}

func (c *Client) generateOnce(ctx context.Context, model, prompt string) (*GenerateResponse, error) {
	body, _ := json.Marshal(GenerateRequest{
		Model:   model,
		Prompt:  prompt,
		Stream:  false,
		Options: &GenerateOptions{NumPredict: 600},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama generate: status %d", resp.StatusCode)
	}

	var result GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Response == "" {
		return nil, fmt.Errorf("ollama generate: empty response (model may be thermal-throttling)")
	}
	return &result, nil
}

func (c *Client) Generate(ctx context.Context, model, prompt string) (string, error) {
	r, err := c.GenerateFull(ctx, model, prompt)
	if err != nil {
		return "", err
	}
	return r.Response, nil
}

func (c *Client) Chat(ctx context.Context, model string, messages []ChatMessage) (string, error) {
	body, _ := json.Marshal(ChatRequest{Model: model, Messages: messages, Stream: false})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama chat: status %d", resp.StatusCode)
	}

	var result ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Message.Content, nil
}

func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama ping: status %d", resp.StatusCode)
	}
	return nil
}
