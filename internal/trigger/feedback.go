package trigger

import (
	"encoding/json"
	"log"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/telemetry"
)

// PendingLookup is the subset of server.PendingStore needed by the feedback handler.
// Defined as an interface to avoid an import cycle (trigger → server).
type PendingLookup interface {
	Get(requestID string) []string
}

// FeedbackRequest is the JSON body for POST /v1/feedback.
// Clients may identify the inference either by:
//   - request_id (returned by /v1/infer) — server resolves knowledge_ids
//   - knowledge_ids directly (explicit, bypasses request_id lookup)
//
// If both are provided, knowledge_ids takes precedence.
type FeedbackRequest struct {
	RequestID    string   `json:"request_id,omitempty"`
	KnowledgeIDs []string `json:"knowledge_ids,omitempty"`
	Outcome      bool     `json:"outcome"`             // true = success
	Evaluator    string   `json:"evaluator,omitempty"` // e.g. "ci", "eval", "human"
}

// FeedbackHandler handles POST /v1/feedback.
type FeedbackHandler struct {
	updater *knowledge.ScoreUpdater
	pending PendingLookup
}

// NewFeedbackHandler creates a FeedbackHandler.
func NewFeedbackHandler(updater *knowledge.ScoreUpdater, pending PendingLookup) *FeedbackHandler {
	return &FeedbackHandler{updater: updater, pending: pending}
}

// ServeHTTP implements http.Handler.
func (h *FeedbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, span := telemetry.Tracer("trigger/feedback").Start(r.Context(), "feedback")
	defer span.End()

	var req FeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	ids := req.KnowledgeIDs
	if len(ids) == 0 && req.RequestID != "" && h.pending != nil {
		ids = h.pending.Get(req.RequestID)
	}
	if len(ids) == 0 {
		span.SetStatus(codes.Error, "no knowledge_ids resolved")
		http.Error(w, "knowledge_ids or valid request_id required", http.StatusBadRequest)
		return
	}

	outcome := "failure"
	if req.Outcome {
		outcome = "success"
	}
	span.SetAttributes(
		attribute.String("feedback_outcome", outcome),
		attribute.String("evaluator", req.Evaluator),
		attribute.Int("knowledge_id_count", len(ids)),
		attribute.String("request_id", req.RequestID),
	)

	h.updater.Send(knowledge.FeedbackEvent{
		KnowledgeIDs: ids,
		Outcome:      req.Outcome,
		Evaluator:    req.Evaluator,
	})
	_ = ctx // span is derived from ctx; suppress unused warning

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"status": "accepted",
		"count":  len(ids),
	}); err != nil {
		log.Printf("feedback: encode response: %v", err)
	}
}

// RegisterFeedbackRoute attaches the feedback endpoint to the mux.
func RegisterFeedbackRoute(mux *http.ServeMux, updater *knowledge.ScoreUpdater, pending PendingLookup) {
	mux.Handle("/v1/feedback", NewFeedbackHandler(updater, pending))
}
