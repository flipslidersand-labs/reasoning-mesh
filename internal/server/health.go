package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

// healthPingTimeout is the per-dependency ping budget.
const healthPingTimeout = 5 * time.Second

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

	resp := healthResponse{Status: "ok", Ollama: "ok", Qdrant: "ok"}

	// Ping both dependencies concurrently, each with its own timeout, so a
	// hanging one cannot starve the other of its budget.
	var wg sync.WaitGroup
	var ollamaErr, qdrantErr error
	ping := func(p Pinger, out *error) {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(r.Context(), healthPingTimeout)
		defer cancel()
		*out = p.Ping(ctx)
	}
	if h.cfg.Ollama != nil {
		wg.Add(1)
		go ping(h.cfg.Ollama, &ollamaErr)
	}
	if h.cfg.Qdrant != nil {
		wg.Add(1)
		go ping(h.cfg.Qdrant, &qdrantErr)
	}
	wg.Wait()

	if ollamaErr != nil {
		resp.Ollama = "error"
		resp.Status = "degraded"
	}
	if qdrantErr != nil {
		resp.Qdrant = "error"
		resp.Status = "degraded"
	}

	code := http.StatusOK
	if resp.Status != "ok" {
		code = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("health: encode response: %v", err)
	}
}
