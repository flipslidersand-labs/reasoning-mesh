// Package server wires all HTTP handlers into a single mux.
package server

import (
	"fmt"
	"net/http"

	"github.com/flipslidersand/reasoning-mesh/internal/router"
)

// Config holds the dependencies needed to build the server.
type Config struct {
	Router      *router.Router
	Health      HealthConfig
	BearerToken string // if non-empty, all non-health endpoints require Authorization: Bearer <token>
}

// Build constructs the HTTP mux with all registered routes.
func Build(cfg Config) http.Handler {
	mux := http.NewServeMux()

	// GET /v1/health — dependency health check (exempt from auth)
	mux.Handle("/v1/health", &healthHandler{cfg: cfg.Health})

	// Legacy alias kept for compatibility
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	// POST /v1/route — task-type-based model routing
	mux.Handle("/v1/route", &routeHandler{router: cfg.Router})

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
		// /v1/health and /healthz are exempt from auth
		if r.URL.Path == "/v1/health" || r.URL.Path == "/healthz" {
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
