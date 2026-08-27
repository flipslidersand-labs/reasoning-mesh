package trigger

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func makeRequest(body string, token string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/trigger", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

// validPayload uses a 7-character hex SHA — the minimum accepted by CommitSHA validation.
const validPayload = `{"commit_sha":"abc1234","diff":"","ci_log":""}`

// Handler contains no token logic; authentication is handled by the bearerAuth
// middleware in internal/server. All requests that reach the handler are accepted.
func TestHandler_AcceptsRequest(t *testing.T) {
	h := NewHandler(nil, nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, makeRequest(validPayload, ""))
	if rec.Code != http.StatusAccepted {
		t.Errorf("want 202, got %d", rec.Code)
	}
}

func TestHandler_AcceptsRequestWithToken(t *testing.T) {
	h := NewHandler(nil, nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, makeRequest(validPayload, "any-token"))
	if rec.Code != http.StatusAccepted {
		t.Errorf("want 202, got %d", rec.Code)
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	h := NewHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/trigger", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rec.Code)
	}
}

func TestHandler_MissingCommitSHA(t *testing.T) {
	h := NewHandler(nil, nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, makeRequest(`{"diff":""}`, "test-token"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}

func TestHandler_BodyTooLarge(t *testing.T) {
	h := NewHandler(nil, nil)

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

func TestHandler_InvalidCommitSHA(t *testing.T) {
	t.Setenv("LLMO_TRIGGER_TOKEN", "test-token")
	h := NewHandler(nil, nil)

	invalid := []string{
		`{"commit_sha":"abc123"}`,                   // 6 chars — too short
		`{"commit_sha":"ABC1234"}`,                   // uppercase
		`{"commit_sha":"../../etc/passwd"}`,          // path traversal
		`{"commit_sha":"abc123\nX-Injected: true"}`,  // log injection
	}
	for _, body := range invalid {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeRequest(body, "test-token"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: want 400, got %d", body, rec.Code)
		}
	}
}
