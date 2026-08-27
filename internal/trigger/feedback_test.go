package trigger_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
	"github.com/flipslidersand/reasoning-mesh/internal/trigger"
)

// validUUID is a well-formed UUID v4 used throughout feedback tests.
const validUUID1 = "550e8400-e29b-41d4-a716-446655440000"
const validUUID2 = "6ba7b810-9dad-41d1-80b4-00c04fd430c8"
const validUUID3 = "f47ac10b-58cc-4372-a567-0e02b2c3d479"

func newTestUpdater() *knowledge.ScoreUpdater {
	return knowledge.NewScoreUpdater((*qdrant.Client)(nil), "test", 8)
}

func TestFeedbackHandler_OK(t *testing.T) {
	u := newTestUpdater()
	u.Start()
	defer u.Stop()

	h := trigger.NewFeedbackHandler(u, nil)
	body, _ := json.Marshal(trigger.FeedbackRequest{
		KnowledgeIDs: []string{validUUID1},
		Outcome:      true,
		Evaluator:    "ci",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d", rec.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "accepted" {
		t.Errorf("status = %v", resp["status"])
	}
}

func TestFeedbackHandler_MissingIDs(t *testing.T) {
	u := newTestUpdater()
	h := trigger.NewFeedbackHandler(u, nil)
	body, _ := json.Marshal(trigger.FeedbackRequest{KnowledgeIDs: nil, Outcome: true})
	req := httptest.NewRequest(http.MethodPost, "/v1/feedback", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestFeedbackHandler_InvalidKnowledgeID(t *testing.T) {
	u := newTestUpdater()
	h := trigger.NewFeedbackHandler(u, nil)
	body, _ := json.Marshal(trigger.FeedbackRequest{
		KnowledgeIDs: []string{"id-001"}, // not a UUID v4
		Outcome:      true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/feedback", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid knowledge_id, got %d", rec.Code)
	}
}

func TestFeedbackHandler_Evaluator(t *testing.T) {
	u := knowledge.NewScoreUpdater(nil, "test", 8)
	h := trigger.NewFeedbackHandler(u, nil)

	body, _ := json.Marshal(trigger.FeedbackRequest{
		KnowledgeIDs: []string{validUUID1},
		Outcome:      false,
		Evaluator:    "eval",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/feedback", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d", rec.Code)
	}
}

func TestFeedbackHandler_MethodNotAllowed(t *testing.T) {
	u := newTestUpdater()
	h := trigger.NewFeedbackHandler(u, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/feedback", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}

func TestFeedbackHandler_BadJSON(t *testing.T) {
	u := newTestUpdater()
	h := trigger.NewFeedbackHandler(u, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/feedback", bytes.NewReader([]byte("{bad")))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestFeedbackHandler_ResponseCountMatchesIDs(t *testing.T) {
	u := newTestUpdater()
	u.Start()
	defer u.Stop()

	h := trigger.NewFeedbackHandler(u, nil)
	body, _ := json.Marshal(trigger.FeedbackRequest{
		KnowledgeIDs: []string{validUUID1, validUUID2, validUUID3},
		Outcome:      true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/feedback", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if count, _ := resp["count"].(float64); int(count) != 3 {
		t.Errorf("want count=3, got %v", resp["count"])
	}
}

// stubPending is a minimal PendingLookup for testing request_id resolution.
type stubPending struct{ ids []string }

func (s *stubPending) Get(_ string) []string { return s.ids }

func TestFeedbackHandler_RequestIDResolution(t *testing.T) {
	u := newTestUpdater()
	u.Start()
	defer u.Stop()

	// IDs from the pending store are already internally generated — they bypass
	// the UUID validation (only directly provided knowledge_ids are validated).
	pending := &stubPending{ids: []string{"id-from-store"}}
	h := trigger.NewFeedbackHandler(u, pending)
	body, _ := json.Marshal(trigger.FeedbackRequest{RequestID: "req-001", Outcome: true})
	req := httptest.NewRequest(http.MethodPost, "/v1/feedback", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d — %s", rec.Code, rec.Body.String())
	}
}

func TestFeedbackHandler_RequestIDNotFound(t *testing.T) {
	u := newTestUpdater()
	h := trigger.NewFeedbackHandler(u, &stubPending{ids: nil})
	body, _ := json.Marshal(trigger.FeedbackRequest{RequestID: "unknown"})
	req := httptest.NewRequest(http.MethodPost, "/v1/feedback", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 when request_id not found, got %d", rec.Code)
	}
}
