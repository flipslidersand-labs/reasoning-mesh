package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/telemetry"
	"github.com/flipslidersand/reasoning-mesh/internal/validate"
)

// maxTriggerBodyBytes is the maximum allowed request body size for /v1/trigger.
// diff and ci_log can be large (monorepo diffs, long build logs) but must be bounded.
const maxTriggerBodyBytes = 16 << 20 // 16 MiB

// TriggerRequest is the JSON body sent by CI on a green build.
type TriggerRequest struct {
	CommitSHA string `json:"commit_sha"`
	Diff      string `json:"diff"`   // output of git diff HEAD~1
	CILog     string `json:"ci_log"` // relevant CI step log
}

// Handler handles POST /v1/trigger and dispatches to the Extractor.
// Authentication is delegated to the bearerAuth middleware in internal/server;
// this handler contains no token logic of its own.
type Handler struct {
	extractor *knowledge.Extractor
	wg        *sync.WaitGroup // optional; tracks in-flight extraction goroutines
}

// NewHandler creates a trigger Handler.
// Pass a non-nil wg to track in-flight extraction goroutines for graceful shutdown.
func NewHandler(extractor *knowledge.Extractor, wg *sync.WaitGroup) *Handler {
	return &Handler{extractor: extractor, wg: wg}
}

// Wait blocks until all in-flight extraction goroutines complete.
// Only useful when wg was provided to NewHandler.
func (h *Handler) Wait() {
	if h.wg != nil {
		h.wg.Wait()
	}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, span := telemetry.Tracer("trigger/ingest").Start(r.Context(), "trigger")
	defer span.End()

	r.Body = http.MaxBytesReader(w, r.Body, maxTriggerBodyBytes)

	var req TriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.SetStatus(codes.Error, err.Error())
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.CommitSHA == "" {
		span.SetStatus(codes.Error, "commit_sha required")
		http.Error(w, "commit_sha required", http.StatusBadRequest)
		return
	}
	if err := validate.CommitSHA(req.CommitSHA); err != nil {
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	span.SetAttributes(
		attribute.String("commit_sha", req.CommitSHA),
		attribute.Int("diff_bytes", len(req.Diff)),
		attribute.Int("ci_log_bytes", len(req.CILog)),
	)

	// Run extraction asynchronously. Use Background so the context is not
	// cancelled when the HTTP handler returns and the request context closes.
	if h.extractor != nil {
		if h.wg != nil {
			h.wg.Add(1)
		}
		go func() {
			if h.wg != nil {
				defer h.wg.Done()
			}
			if err := h.extractor.Run(context.Background(), req.CommitSHA, req.Diff, req.CILog); err != nil {
				log.Printf("trigger: extractor error for %s: %v", req.CommitSHA, err)
			}
		}()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status":     "accepted",
		"commit_sha": req.CommitSHA,
	}); err != nil {
		log.Printf("trigger: encode response: %v", err)
	}
}

// RegisterRoutes attaches the trigger endpoint to the given mux.
// Pass a non-nil wg to track in-flight extraction goroutines for graceful shutdown.
func RegisterRoutes(mux *http.ServeMux, extractor *knowledge.Extractor, wg *sync.WaitGroup) {
	h := NewHandler(extractor, wg)
	mux.Handle("/v1/trigger", h)
}
