package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Pinger is implemented by both ollama.Client and qdrant.Client.
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthConfig holds the dependencies for the health handler.
type HealthConfig struct {
	Ollama Pinger
	Qdrant Pinger
}

type healthHandler struct {
	cfg HealthConfig
}

type healthResponse struct {
	Status string `json:"status"`
	Ollama string `json:"ollama"`
	Qdrant string `json:"qdrant"`
}

func (h *healthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp := healthResponse{Status: "ok", Ollama: "ok", Qdrant: "ok"}

	if h.cfg.Ollama != nil {
		if err := h.cfg.Ollama.Ping(ctx); err != nil {
			resp.Ollama = "error"
			resp.Status = "degraded"
		}
	}
	if h.cfg.Qdrant != nil {
		if err := h.cfg.Qdrant.Ping(ctx); err != nil {
			resp.Qdrant = "error"
			resp.Status = "degraded"
		}
	}

	code := http.StatusOK
	if resp.Status != "ok" {
		code = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(resp)
}
