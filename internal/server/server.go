// Package server wires all HTTP handlers into a single mux.
package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/router"
	"github.com/flipslidersand/reasoning-mesh/internal/trigger"
)

// Config holds the dependencies needed to build the server.
type Config struct {
	Router       *router.Router
	Extractor    *knowledge.Extractor
	ScoreUpdater *knowledge.ScoreUpdater
	Retriever    InferRetriever
	Pending      *PendingStore   // if nil, a new store is created
	Health       HealthConfig    // Ollama + Qdrant pingers for GET /v1/health
	BearerToken  string          // if non-empty, protects /v1/route and /v1/infer (not /v1/trigger or /v1/feedback which use their own tokens)
	ExtractWG    *sync.WaitGroup // if non-nil, trigger extraction goroutines are tracked for graceful shutdown
	// LifecycleCtx is the server's lifecycle context. When cancelled (e.g. on
	// SIGTERM), in-flight extraction goroutines spawned by /v1/trigger will
	// receive the cancellation signal and exit early. If nil, context.Background
	// is used (no cancellation — goroutines run to completion or until process exit).
	LifecycleCtx context.Context
}

// Build constructs the HTTP mux with all registered routes.
func Build(cfg Config) http.Handler {
	mux := http.NewServeMux()

	pending := cfg.Pending
	if pending == nil {
		pending = NewPendingStore()
	}

	// GET /v1/health — Ollama + Qdrant ping (exempt from auth)
	mux.Handle("/v1/health", &healthHandler{cfg: cfg.Health})

	// POST /v1/route — keyword-based task routing
	mux.Handle("/v1/route", &routeHandler{router: cfg.Router})

	// Legacy health alias
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	// Trigger: POST /v1/trigger (CI → knowledge ingest)
	trigger.RegisterRoutes(mux, cfg.Extractor, cfg.ExtractWG, cfg.LifecycleCtx)

	// Feedback: POST /v1/feedback (inference outcome → score update)
	trigger.RegisterFeedbackRoute(mux, cfg.ScoreUpdater, pending)

	// Infer: POST /v1/infer (RAG-augmented routing inference)
	mux.Handle("/v1/infer", &inferHandler{
		router:    cfg.Router,
		retriever: cfg.Retriever,
		updater:   cfg.ScoreUpdater,
		pending:   pending,
	})

	var handler http.Handler = mux
	if cfg.BearerToken != "" {
		handler = bearerAuth(cfg.BearerToken, mux)
	}
	return logRequests(handler)
}

// Addr returns the listen address from host and port.
func Addr(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}

// bearerAuthExempt lists paths that have their own per-handler auth and must not
// be double-checked by the mux-level ORCH_TOKEN middleware.
var bearerAuthExempt = map[string]bool{
	"/v1/health":   true,
	"/healthz":     true,
	"/v1/trigger":  true, // uses LLMO_TRIGGER_TOKEN via trigger.Handler
	"/v1/feedback": true, // uses its own validation
}

func bearerAuth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if bearerAuthExempt[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
