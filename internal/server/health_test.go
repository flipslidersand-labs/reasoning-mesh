package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type hangPinger struct{}

func (hangPinger) Ping(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

type okPinger struct{}

func (okPinger) Ping(ctx context.Context) error { return ctx.Err() }

// A hanging Ollama must not make Qdrant report error (independent timeouts),
// and the pings must run concurrently.
func TestHealth_HangingOllamaDoesNotPoisonQdrant(t *testing.T) {
	h := &healthHandler{cfg: HealthConfig{Ollama: hangPinger{}, Qdrant: okPinger{}}}
	rec := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	elapsed := time.Since(start)

	var resp healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable || resp.Ollama != "error" {
		t.Errorf("code=%d ollama=%q, want 503/error", rec.Code, resp.Ollama)
	}
	if resp.Qdrant != "ok" {
		t.Errorf("qdrant=%q, want ok", resp.Qdrant)
	}
	if elapsed > 7*time.Second {
		t.Errorf("elapsed %v too long", elapsed)
	}
}

type slowPinger struct{ d time.Duration }

func (p slowPinger) Ping(ctx context.Context) error {
	select {
	case <-time.After(p.d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Two 3s pings are fine concurrently (each has its own 5s budget) but would
// exceed a shared 5s budget if run sequentially.
func TestHealth_PingsRunConcurrently(t *testing.T) {
	h := &healthHandler{cfg: HealthConfig{Ollama: slowPinger{3 * time.Second}, Qdrant: slowPinger{3 * time.Second}}}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("code=%d, want 200 (pings should have independent budgets)", rec.Code)
	}
}

// Both hanging: concurrent pings answer in ~5s; sequential ones would take ~10s.
func TestHealth_BothHangingRespondsWithinOneBudget(t *testing.T) {
	h := &healthHandler{cfg: HealthConfig{Ollama: hangPinger{}, Qdrant: hangPinger{}}}
	rec := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if elapsed := time.Since(start); elapsed > 7*time.Second {
		t.Errorf("elapsed %v, pings appear sequential", elapsed)
	}
}
