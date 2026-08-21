package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/router"
	"github.com/flipslidersand/reasoning-mesh/internal/server"
)

// stubAdapter always returns a fixed response.
type stubAdapter struct{ model string }

func (s *stubAdapter) Name() string { return s.model }
func (s *stubAdapter) Generate(_ context.Context, _ string) (router.Response, error) {
	return router.Response{Text: "stub answer", Model: s.model, PromptTokens: 10, TotalTokens: 20}, nil
}

// stubRetriever returns no items.
type stubRetriever struct{}

func (stubRetriever) Retrieve(_ context.Context, _ string, _ eval.TaskType, _ int) ([]eval.KnowledgeItem, error) {
	return nil, nil
}

func buildTestServer(token string) http.Handler {
	stub := &stubAdapter{model: "stub"}
	r := router.New(router.Config{Default: stub})
	updater := knowledge.NewScoreUpdater(nil, "col", 8)
	updater.Start()

	return server.Build(server.Config{
		Router:       r,
		Extractor:    nil,
		ScoreUpdater: updater,
		Retriever:    stubRetriever{},
		BearerToken:  token,
	})
}

func TestHealthz(t *testing.T) {
	h := buildTestServer("")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("healthz status %d", rec.Code)
	}
}

func TestInfer_MissingPrompt(t *testing.T) {
	h := buildTestServer("")
	body, _ := json.Marshal(map[string]string{"task_type": "debugging"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/infer", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestInfer_OK(t *testing.T) {
	h := buildTestServer("")
	body, _ := json.Marshal(server.InferRequest{Prompt: "fix the bug", TaskType: "debugging"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/infer", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp server.InferResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Answer != "stub answer" {
		t.Errorf("unexpected answer: %q", resp.Answer)
	}
	if resp.Model != "stub" {
		t.Errorf("unexpected model: %q", resp.Model)
	}
}

func TestInfer_MethodNotAllowed(t *testing.T) {
	h := buildTestServer("")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/infer", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestBearerAuth_HealthzExempt(t *testing.T) {
	h := buildTestServer("secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("healthz should be exempt, got %d", rec.Code)
	}
}

func TestBearerAuth_Rejected(t *testing.T) {
	h := buildTestServer("secret")
	body, _ := json.Marshal(server.InferRequest{Prompt: "hello"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/infer", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer wrong")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestBearerAuth_Accepted(t *testing.T) {
	h := buildTestServer("secret")
	body, _ := json.Marshal(server.InferRequest{Prompt: "hello"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/infer", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
