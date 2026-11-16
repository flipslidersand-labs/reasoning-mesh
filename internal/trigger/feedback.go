package trigger

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/flipslidersand/reasoning-mesh/internal/bearer"
	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/telemetry"
	"github.com/flipslidersand/reasoning-mesh/internal/validate"
)

// maxFeedbackBodyBytes is the maximum allowed request body size for /v1/feedback.
const maxFeedbackBodyBytes = 1 << 20 // 1 MiB

// PendingLookup is the subset of server.PendingStore needed by the feedback handler.
// Defined as an interface to avoid an import cycle (trigger → server).
type PendingLookup interface {
	// Claim reserves the entry without removing it. busy reports that another
	// request holds it; ids==nil && !busy means not found/expired. A successful
	// claim must be followed by exactly one of Commit or Release.
	Claim(requestID string) (ids []string, busy bool)
	// Commit removes the claimed entry (feedback was enqueued).
	Commit(requestID string)
	// Release gives the claim back so the client can retry after a 503.
	Release(requestID string)
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
	Outcome      *bool    `json:"outcome"`             // required; true = success (pointer so omission/null is detectable)
	Evaluator    string   `json:"evaluator,omitempty"` // e.g. "ci", "eval", "human"
}

// FeedbackHandler handles POST /v1/feedback.
type FeedbackHandler struct {
	updater *knowledge.ScoreUpdater
	pending PendingLookup
	token   string // expected Bearer token; empty = auth disabled (matches /v1/trigger's fail-safe var)
}

// NewFeedbackHandler creates a FeedbackHandler.
// The token is read from LLMO_TRIGGER_TOKEN (the same token required for
// POST /v1/trigger) so a single secret protects both write endpoints and
// unauthenticated callers cannot poison knowledge scores via /v1/feedback.
func NewFeedbackHandler(updater *knowledge.ScoreUpdater, pending PendingLookup) *FeedbackHandler {
	return &FeedbackHandler{updater: updater, pending: pending, token: os.Getenv("LLMO_TRIGGER_TOKEN")}
}

// ServeHTTP implements http.Handler.
func (h *FeedbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate Bearer token when configured (see NewFeedbackHandler).
	if h.token != "" {
		auth := r.Header.Get("Authorization")
		if !bearer.Matches(auth, h.token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	ctx, span := telemetry.Tracer("trigger/feedback").Start(r.Context(), "feedback")
	defer span.End()

	r.Body = http.MaxBytesReader(w, r.Body, maxFeedbackBodyBytes)

	var req FeedbackRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		span.SetStatus(codes.Error, err.Error())
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Reject before touching the pending store so a bad body does not burn request_id.
	if req.Outcome == nil {
		span.SetStatus(codes.Error, "outcome required")
		http.Error(w, "bad request: outcome required", http.StatusBadRequest)
		return
	}

	// When the client provides knowledge_ids directly, validate them as UUIDs (hyphen/case-insensitive) and normalize
	// to the hyphenated lowercase form Qdrant returns.
	// IDs resolved from the pending store were already validated at ingest time.
	if len(req.KnowledgeIDs) > 0 {
		if err := validate.KnowledgeIDs(req.KnowledgeIDs); err != nil {
			span.SetStatus(codes.Error, err.Error())
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		norm := make([]string, len(req.KnowledgeIDs))
		for i, id := range req.KnowledgeIDs {
			norm[i] = validate.NormalizeKnowledgeID(id)
		}
		req.KnowledgeIDs = norm
	}

	ids := req.KnowledgeIDs
	claimed := false
	if len(ids) == 0 && req.RequestID != "" && h.pending != nil {
		got, busy := h.pending.Claim(req.RequestID)
		if busy {
			// Another request with the same request_id is in flight; the client
			// may retry (not a permanent 400).
			span.SetStatus(codes.Error, "request_id in flight")
			writeDropped(w, 0)
			return
		}
		ids = got
		// Release is a no-op after Commit, so a plain defer guarantees the
		// claim is never leaked (including on 400 / panic paths).
		claimed = true
		defer h.pending.Release(req.RequestID)
	}
	if len(ids) == 0 {
		span.SetStatus(codes.Error, "no knowledge_ids resolved")
		http.Error(w, "knowledge_ids or valid request_id required", http.StatusBadRequest)
		return
	}

	outcome := "failure"
	if *req.Outcome {
		outcome = "success"
	}
	span.SetAttributes(
		attribute.String("feedback_outcome", outcome),
		attribute.String("evaluator", req.Evaluator),
		attribute.Int("knowledge_id_count", len(ids)),
		attribute.String("request_id", req.RequestID),
	)

	enqueued := h.updater.Send(knowledge.FeedbackEvent{
		KnowledgeIDs: ids,
		Outcome:      *req.Outcome,
		Evaluator:    req.Evaluator,
	})
	_ = ctx // span is derived from ctx; suppress unused warning

	if !enqueued {
		// The claim is released by the defer above, so the request_id is not burned.
		span.SetStatus(codes.Error, "score updater buffer full, feedback dropped")
		writeDropped(w, len(ids))
		return
	}
	if claimed {
		h.pending.Commit(req.RequestID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"status":   "accepted",
		"accepted": true,
		"count":    len(ids),
	}); err != nil {
		log.Printf("feedback: encode response: %v", err)
	}
}

// RegisterFeedbackRoute attaches the feedback endpoint to the mux.
func RegisterFeedbackRoute(mux *http.ServeMux, updater *knowledge.ScoreUpdater, pending PendingLookup) {
	mux.Handle("/v1/feedback", NewFeedbackHandler(updater, pending))
}

// writeDropped writes the 503 "dropped" response.
func writeDropped(w http.ResponseWriter, count int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"status":   "dropped",
		"accepted": false,
		"count":    count,
	}); err != nil {
		log.Printf("feedback: encode response: %v", err)
	}
}
