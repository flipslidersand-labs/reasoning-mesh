package trigger

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// LLMO_TRIGGER_TOKEN must be set; server refuses to start without it.
	os.Setenv("LLMO_TRIGGER_TOKEN", "test-token")
	os.Exit(m.Run())
}

func makeRequest(body string, token string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/trigger", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

const validPayload = `{"commit_sha":"abc123","diff":"","ci_log":""}`

func TestHandler_WithToken_AcceptsValidRequest(t *testing.T) {
	t.Setenv("LLMO_TRIGGER_TOKEN", "test-token")
	h := NewHandler(nil, nil, nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, makeRequest(validPayload, "test-token"))
	if rec.Code != http.StatusAccepted {
		t.Errorf("want 202, got %d", rec.Code)
	}
}

func TestHandler_WithToken_ValidBearer(t *testing.T) {
	t.Setenv("LLMO_TRIGGER_TOKEN", "secret")
	h := NewHandler(nil, nil, nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, makeRequest(validPayload, "secret"))
	if rec.Code != http.StatusAccepted {
		t.Errorf("want 202, got %d", rec.Code)
	}
}

func TestHandler_WithToken_WrongToken(t *testing.T) {
	t.Setenv("LLMO_TRIGGER_TOKEN", "secret")
	h := NewHandler(nil, nil, nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, makeRequest(validPayload, "wrong"))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rec.Code)
	}
}

func TestHandler_WithToken_MissingHeader(t *testing.T) {
	t.Setenv("LLMO_TRIGGER_TOKEN", "secret")
	h := NewHandler(nil, nil, nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, makeRequest(validPayload, ""))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rec.Code)
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	h := NewHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/trigger", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rec.Code)
	}
}

func TestHandler_MissingCommitSHA(t *testing.T) {
	t.Setenv("LLMO_TRIGGER_TOKEN", "test-token")
	h := NewHandler(nil, nil, nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, makeRequest(`{"diff":""}`, "test-token"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}

func TestHandler_BodyTooLarge(t *testing.T) {
	t.Setenv("LLMO_TRIGGER_TOKEN", "test-token")
	h := NewHandler(nil, nil, nil)

	// Build a payload larger than 16 MiB.
	large := make([]byte, maxTriggerBodyBytes+1)
	for i := range large {
		large[i] = 'x'
	}
	body := `{"commit_sha":"abc","diff":"` + string(large) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/trigger", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("want 413, got %d", rec.Code)
	}
}

func TestHandler_CancelledContext_DoesNotBlock(t *testing.T) {
	t.Setenv("LLMO_TRIGGER_TOKEN", "test-token")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled; goroutine should not block
	h := NewHandler(nil, nil, ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, makeRequest(validPayload, "test-token"))
	if rec.Code != http.StatusAccepted {
		t.Errorf("want 202, got %d", rec.Code)
	}
}
