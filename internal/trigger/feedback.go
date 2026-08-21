package trigger

import (
	"encoding/json"
	"net/http"

	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
)

// FeedbackRequest is the JSON body for POST /v1/feedback.
type FeedbackRequest struct {
	KnowledgeIDs []string `json:"knowledge_ids"`
	Outcome      bool     `json:"outcome"` // true = success
}

// FeedbackHandler handles POST /v1/feedback.
type FeedbackHandler struct {
	updater *knowledge.ScoreUpdater
}

// NewFeedbackHandler creates a FeedbackHandler.
func NewFeedbackHandler(updater *knowledge.ScoreUpdater) *FeedbackHandler {
	return &FeedbackHandler{updater: updater}
}

// ServeHTTP implements http.Handler.
func (h *FeedbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req FeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.KnowledgeIDs) == 0 {
		http.Error(w, "knowledge_ids required", http.StatusBadRequest)
		return
	}

	h.updater.Send(knowledge.FeedbackEvent{
		KnowledgeIDs: req.KnowledgeIDs,
		Outcome:      req.Outcome,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "accepted",
		"count":  len(req.KnowledgeIDs),
	})
}

// RegisterFeedbackRoute attaches the feedback endpoint to the mux.
func RegisterFeedbackRoute(mux *http.ServeMux, updater *knowledge.ScoreUpdater) {
	mux.Handle("/v1/feedback", NewFeedbackHandler(updater))
}
