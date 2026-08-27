package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/router"
	"github.com/flipslidersand/reasoning-mesh/internal/server"
)

// stubRetriever returns a fixed set of KnowledgeItems.
type stubRetriever struct {
	items []eval.KnowledgeItem
}

func (s *stubRetriever) Retrieve(_ context.Context, _ string, _ eval.TaskType, _ int) ([]eval.KnowledgeItem, error) {
	return s.items, nil
}

func buildInferServer(retriever server.InferRetriever, token string) http.Handler {
	stub := &stubAdapter{name: "stub-model"}
	r := router.New(router.Config{Default: stub})
	return server.Build(server.Config{
		Router:      r,
		Retriever:   retriever,
		BearerToken: token,
	})
}

// --- POST /v1/infer ---

func TestInfer_OK_NoRAG(t *testing.T) {
	srv := buildInferServer(nil, "")
	body, _ := json.Marshal(server.InferRequest{Prompt: "implement auth middleware", TaskType: "implementation"})
	req := httptest.NewRequest(http.MethodPost, "/v1/infer", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d — %s", rec.Code, rec.Body.String())
	}
	var resp server.InferResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Answer == "" {
		t.Error("expected non-empty answer")
	}
	if resp.Model != "stub-model" {
		t.Errorf("want model stub-model, got %q", resp.Model)
	}
	if resp.RequestID == "" {
		t.Error("expected non-empty request_id")
	}
}

func TestInfer_OK_WithRAG(t *testing.T) {
	retriever := &stubRetriever{items: []eval.KnowledgeItem{
		{ID: "ki-001", Content: "use sync.Mutex for concurrency"},
		{ID: "ki-002", Content: "prefer context.WithTimeout"},
	}}
	srv := buildInferServer(retriever, "")
	body, _ := json.Marshal(server.InferRequest{Prompt: "fix race condition", TaskType: "debugging", TopK: 2})
	req := httptest.NewRequest(http.MethodPost, "/v1/infer", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d — %s", rec.Code, rec.Body.String())
	}
	var resp server.InferResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.KnowledgeIDs) != 2 {
		t.Errorf("want 2 knowledge_ids, got %d", len(resp.KnowledgeIDs))
	}
	if resp.KnowledgeIDs[0] != "ki-001" {
		t.Errorf("first knowledge_id = %q, want ki-001", resp.KnowledgeIDs[0])
	}
}

func TestInfer_DefaultTaskType(t *testing.T) {
	// task_type omitted → defaults to implementation
	srv := buildInferServer(nil, "")
	body, _ := json.Marshal(map[string]string{"prompt": "add a login endpoint"})
	req := httptest.NewRequest(http.MethodPost, "/v1/infer", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

func TestInfer_EmptyPrompt(t *testing.T) {
	srv := buildInferServer(nil, "")
	body, _ := json.Marshal(server.InferRequest{Prompt: "   "})
	req := httptest.NewRequest(http.MethodPost, "/v1/infer", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestInfer_BadJSON(t *testing.T) {
	srv := buildInferServer(nil, "")
	req := httptest.NewRequest(http.MethodPost, "/v1/infer", bytes.NewReader([]byte("{bad")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestInfer_MethodNotAllowed(t *testing.T) {
	srv := buildInferServer(nil, "")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/infer", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}

func TestInfer_BearerAuth(t *testing.T) {
	srv := buildInferServer(nil, "tok")
	body, _ := json.Marshal(server.InferRequest{Prompt: "hello"})

	// no token → 401
	req := httptest.NewRequest(http.MethodPost, "/v1/infer", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}

	// correct token → 200
	req = httptest.NewRequest(http.MethodPost, "/v1/infer", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 with token, got %d", rec.Code)
	}
}

func TestInfer_BodyTooLarge(t *testing.T) {
	srv := buildInferServer(nil, "")

	// Build a payload larger than 1 MiB.
	large := make([]byte, (1<<20)+1)
	for i := range large {
		large[i] = 'x'
	}
	body := `{"prompt":"` + string(large) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/infer", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d", rec.Code)
	}
}
