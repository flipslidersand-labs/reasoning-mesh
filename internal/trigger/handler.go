package trigger

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
)

// TriggerRequest is the JSON body sent by CI on a green build.
type TriggerRequest struct {
	CommitSHA string `json:"commit_sha"`
	Diff      string `json:"diff"`    // output of git diff HEAD~1
	CILog     string `json:"ci_log"`  // relevant CI step log
}

// Handler handles POST /v1/trigger and dispatches to the Extractor.
type Handler struct {
	extractor *knowledge.Extractor
}

// NewHandler creates a trigger Handler.
func NewHandler(extractor *knowledge.Extractor) *Handler {
	return &Handler{extractor: extractor}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req TriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.CommitSHA == "" {
		http.Error(w, "commit_sha required", http.StatusBadRequest)
		return
	}

	// Run extraction asynchronously so the CI webhook returns immediately.
	if h.extractor != nil {
		go func() {
			if err := h.extractor.Run(r.Context(), req.CommitSHA, req.Diff, req.CILog); err != nil {
				log.Printf("trigger: extractor error for %s: %v", req.CommitSHA, err)
			}
		}()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":     "accepted",
		"commit_sha": req.CommitSHA,
	})
}

// RegisterRoutes attaches the trigger endpoint to the given mux.
func RegisterRoutes(mux *http.ServeMux, extractor *knowledge.Extractor) {
	h := NewHandler(extractor)
	mux.Handle("/v1/trigger", h)
}
