package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// FeedbackSender is called after each eval run to record which knowledge was useful.
type FeedbackSender interface {
	Send(ctx context.Context, knowledgeIDs []string, success bool, evaluator string) error
}

// feedbackPayload is the JSON body for POST /v1/feedback.
type feedbackPayload struct {
	KnowledgeIDs []string `json:"knowledge_ids"`
	Outcome      bool     `json:"outcome"`
	Evaluator    string   `json:"evaluator"`
}

// HTTPFeedbackSender posts to /v1/feedback on the llmo server.
type HTTPFeedbackSender struct {
	serverURL string
	token     string
	client    *http.Client
}

// NewHTTPFeedbackSender creates a sender that posts to <serverURL>/v1/feedback.
func NewHTTPFeedbackSender(serverURL, token string) *HTTPFeedbackSender {
	return &HTTPFeedbackSender{
		serverURL: serverURL,
		token:     token,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Send posts a feedback event; non-fatal (logs errors but doesn't block eval).
func (s *HTTPFeedbackSender) Send(ctx context.Context, knowledgeIDs []string, success bool, evaluator string) error {
	if len(knowledgeIDs) == 0 {
		return nil
	}
	body, _ := json.Marshal(feedbackPayload{
		KnowledgeIDs: knowledgeIDs,
		Outcome:      success,
		Evaluator:    evaluator,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.serverURL+"/v1/feedback", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("feedback: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("feedback: post: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("feedback: server returned %d", resp.StatusCode)
	}
	return nil
}
