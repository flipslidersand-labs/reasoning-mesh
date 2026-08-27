package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/router"
	"github.com/flipslidersand/reasoning-mesh/internal/server"
)

func TestMain(m *testing.M) {
	// LLMO_TRIGGER_TOKEN must be set; server refuses to start without it.
	os.Setenv("LLMO_TRIGGER_TOKEN", "test-token")
	os.Exit(m.Run())
}

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

// inferTaskType routing tests — verify both status and the selected model name.
func TestRoute_TaskTypeInference(t *testing.T) {
	cases := []struct {
		task      string
		wantModel string // adapter Name() that should handle this task
	}{
		// English keywords
		{"fix this bug", "debug-adapter"},
		{"write tests for this", "test-adapter"},
		{"design the architecture", "arch-adapter"},
		{"implement the feature", "impl-adapter"},
		// Japanese keywords (#87)
		{"このCIエラーの原因を調べて", "debug-adapter"},
		{"テストを書いて", "test-adapter"},
		{"アーキテクチャを設計して", "arch-adapter"},
		{"実装して", "impl-adapter"},
		{"バグを修正して", "debug-adapter"},
		{"スキーマを設計して", "arch-adapter"},
	}

	r := router.New(router.Config{
		Debugging:      &stubAdapter{name: "debug-adapter"},
		Testing:        &stubAdapter{name: "test-adapter"},
		Architecture:   &stubAdapter{name: "arch-adapter"},
		Implementation: &stubAdapter{name: "impl-adapter"},
		Default:        &stubAdapter{name: "impl-adapter"},
	})
	srv := server.Build(server.Config{Router: r})

	for _, tc := range cases {
		body, _ := json.Marshal(server.RouteRequest{Task: tc.task})
		req := httptest.NewRequest(http.MethodPost, "/v1/route", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%q: status %d", tc.task, rec.Code)
			continue
		}
		var resp server.RouteResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Model != tc.wantModel {
			t.Errorf("%q: want model %q, got %q", tc.task, tc.wantModel, resp.Model)
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

func TestRoute_BodyTooLarge(t *testing.T) {
	srv := buildTestServer("")

	// Build a payload larger than 1 MiB.
	large := make([]byte, (1<<20)+1)
	for i := range large {
		large[i] = 'x'
	}
	body := `{"task":"` + string(large) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/route", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d", rec.Code)
	}
}
