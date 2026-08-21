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

// stubAdapter is a deterministic ModelAdapter for testing.
type stubAdapter struct{ name string }

func (s *stubAdapter) Name() string { return s.name }
func (s *stubAdapter) Generate(_ context.Context, prompt string) (router.Response, error) {
	return router.Response{Text: "answer:" + prompt, Model: s.name, TotalTokens: 10}, nil
}

// okPinger always returns nil.
type okPinger struct{}

func (okPinger) Ping(_ context.Context) error { return nil }

// errPinger always returns an error.
type errPinger struct{}

func (errPinger) Ping(_ context.Context) error { return context.DeadlineExceeded }

func buildTestServer(token string) http.Handler {
	stub := &stubAdapter{name: "stub"}
	r := router.New(router.Config{Default: stub})
	return server.Build(server.Config{
		Router: r,
		Health: server.HealthConfig{Ollama: okPinger{}, Qdrant: okPinger{}},
		BearerToken: token,
	})
}

// --- GET /v1/health ---

func TestHealth_OK(t *testing.T) {
	srv := buildTestServer("")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["status"] != "ok" {
		t.Errorf("status = %q", body["status"])
	}
	if body["ollama"] != "ok" || body["qdrant"] != "ok" {
		t.Errorf("deps: %v", body)
	}
}

func TestHealth_Degraded(t *testing.T) {
	stub := &stubAdapter{name: "stub"}
	r := router.New(router.Config{Default: stub})
	srv := server.Build(server.Config{
		Router: r,
		Health: server.HealthConfig{Ollama: errPinger{}, Qdrant: okPinger{}},
	})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["status"] != "degraded" {
		t.Errorf("status = %q", body["status"])
	}
	if body["ollama"] != "error" {
		t.Errorf("ollama = %q", body["ollama"])
	}
}

func TestHealth_MethodNotAllowed(t *testing.T) {
	srv := buildTestServer("")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/health", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}

// --- POST /v1/route ---

func TestRoute_OK(t *testing.T) {
	srv := buildTestServer("")

	body, _ := json.Marshal(server.RouteRequest{
		Task:    "このCIエラーの原因を調べて",
		Context: &server.RouteContext{Repo: "foo", Language: "go", Branch: "main"},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/route", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d — %s", rec.Code, rec.Body.String())
	}
	var resp server.RouteResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Answer == "" {
		t.Error("empty answer")
	}
	if resp.Model == "" {
		t.Error("empty model")
	}
}

func TestRoute_EmptyTask(t *testing.T) {
	srv := buildTestServer("")
	body, _ := json.Marshal(server.RouteRequest{Task: "  "})
	req := httptest.NewRequest(http.MethodPost, "/v1/route", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestRoute_BadJSON(t *testing.T) {
	srv := buildTestServer("")
	req := httptest.NewRequest(http.MethodPost, "/v1/route", bytes.NewReader([]byte("{bad")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestRoute_MethodNotAllowed(t *testing.T) {
	srv := buildTestServer("")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/route", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}

// inferTaskType routing smoke tests via the HTTP layer
func TestRoute_TaskTypeInference(t *testing.T) {
	cases := []struct {
		task     string
		wantType eval.TaskType
	}{
		{"このCIエラーの原因を調べて", eval.TaskDebugging},
		{"テストを書いて", eval.TaskTesting},
		{"アーキテクチャを設計して", eval.TaskArchitecture},
		{"実装して", eval.TaskImplementation},
	}
	stub := &stubAdapter{name: "stub"}
	r := router.New(router.Config{Default: stub})
	srv := server.Build(server.Config{Router: r})

	for _, tc := range cases {
		body, _ := json.Marshal(server.RouteRequest{Task: tc.task})
		req := httptest.NewRequest(http.MethodPost, "/v1/route", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%q: status %d", tc.task, rec.Code)
		}
	}
}

// --- Bearer auth ---

func TestBearerAuth_Missing(t *testing.T) {
	srv := buildTestServer("secret")
	body, _ := json.Marshal(server.RouteRequest{Task: "hello"})
	req := httptest.NewRequest(http.MethodPost, "/v1/route", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestBearerAuth_Valid(t *testing.T) {
	srv := buildTestServer("secret")
	body, _ := json.Marshal(server.RouteRequest{Task: "hello"})
	req := httptest.NewRequest(http.MethodPost, "/v1/route", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

func TestBearerAuth_HealthExempt(t *testing.T) {
	srv := buildTestServer("secret")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/v1/health should be auth-exempt, got %d", rec.Code)
	}
}
