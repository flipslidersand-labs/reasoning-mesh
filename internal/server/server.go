// Package server wires all HTTP handlers into a single mux.
package server

import (
	"fmt"
	"net/http"

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
	Pending      *PendingStore // if nil, a new store is created
	BearerToken  string        // if non-empty, all endpoints require Authorization: Bearer <token>
}

// Build constructs the HTTP mux with all registered routes.
func Build(cfg Config) http.Handler {
	mux := http.NewServeMux()

	pending := cfg.Pending
	if pending == nil {
		pending = NewPendingStore()
	}

	// Health
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	// Trigger: POST /v1/trigger (CI → knowledge ingest)
	trigger.RegisterRoutes(mux, cfg.Extractor)

	// Feedback: POST /v1/feedback (inference outcome → score update)
	trigger.RegisterFeedbackRoute(mux, cfg.ScoreUpdater, pending)

	// Infer: POST /v1/infer (RAG-augmented routing inference)
	mux.Handle("/v1/infer", &inferHandler{
		router:    cfg.Router,
		retriever: cfg.Retriever,
		updater:   cfg.ScoreUpdater,
		pending:   pending,
	})

	if cfg.BearerToken != "" {
		return bearerAuth(cfg.BearerToken, mux)
	}
	return mux
}

// Addr returns the listen address from host and port.
func Addr(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}

func bearerAuth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /healthz is exempt from auth
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		expected := "Bearer " + token
		if auth != expected {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
