package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

// healthPingTimeout is the per-dependency ping budget.
const healthPingTimeout = 5 * time.Second

// healthCacheTTL is how long a health result (healthy or not) is reused
// before the dependencies are pinged again. The endpoint is unauthenticated,
// so this bounds the outbound ping rate regardless of request volume.
const healthCacheTTL = 5 * time.Second

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

	// now is the clock; nil means time.Now. Tests inject a fake.
	now func() time.Time

	// mu guards the cache and is held while pinging so concurrent requests
	// share one round of pings (bounded by healthPingTimeout).
	mu         sync.Mutex
	cachedAt   time.Time
	cachedOK   bool // true once a result has been stored
	cachedCode int
	cachedBody []byte
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

	code, payload := h.result(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if _, err := w.Write(payload); err != nil {
		log.Printf("health: write response: %v", err)
	}
}

// result returns the cached status and body, refreshing them when the cache is
// empty or at least healthCacheTTL old (age == TTL counts as expired; age is measured from when the pings finished).
func (h *healthHandler) result(parent context.Context) (int, []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()

	clock := h.now
	if clock == nil {
		clock = time.Now
	}
	if h.cachedOK && clock().Sub(h.cachedAt) < healthCacheTTL {
		return h.cachedCode, h.cachedBody
	}

	// Detach from the first requester's cancellation so an aborted request
	// cannot cache a spurious failure for everyone else.
	ctx := context.WithoutCancel(parent)
	resp := healthResponse{Status: "ok", Ollama: "ok", Qdrant: "ok"}

	// Ping both dependencies concurrently, each with its own timeout, so a
	// hanging one cannot starve the other of its budget.
	var wg sync.WaitGroup
	var ollamaErr, qdrantErr error
	ping := func(p Pinger, out *error) {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(ctx, healthPingTimeout)
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
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(resp); err != nil {
		log.Printf("health: encode response: %v", err)
	}
	// Stamp after the pings finish: the TTL is the age of the result, so a
	// slow ping cannot leave the cache already expired when it is written.
	h.cachedAt, h.cachedOK, h.cachedCode, h.cachedBody = clock(), true, code, buf.Bytes()
	return code, h.cachedBody
}
