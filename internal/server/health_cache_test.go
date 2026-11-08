package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// togglePinger counts calls and fails while fail is set.
type togglePinger struct {
	calls atomic.Int32
	fail  atomic.Bool
}

func (p *togglePinger) Ping(context.Context) error {
	p.calls.Add(1)
	if p.fail.Load() {
		return errors.New("down")
	}
	return nil
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func newCachedHealth(p *togglePinger) (*healthHandler, *fakeClock) {
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	return &healthHandler{cfg: HealthConfig{Ollama: p, Qdrant: okPinger{}}, now: clk.now}, clk
}

func getHealth(h *healthHandler) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	return rec
}

func TestHealthCache_WithinTTLDoesNotPing(t *testing.T) {
	p := &togglePinger{}
	h, clk := newCachedHealth(p)
	first := getHealth(h)
	clk.t = clk.t.Add(healthCacheTTL - time.Nanosecond)
	second := getHealth(h)
	if n := p.calls.Load(); n != 1 {
		t.Fatalf("pings = %d, want 1", n)
	}
	if first.Code != second.Code || first.Body.String() != second.Body.String() {
		t.Fatalf("cached response differs: %d %q vs %d %q", first.Code, first.Body, second.Code, second.Body)
	}
}

func TestHealthCache_AfterTTLPingsAgain(t *testing.T) {
	p := &togglePinger{}
	h, clk := newCachedHealth(p)
	getHealth(h)
	clk.t = clk.t.Add(healthCacheTTL + time.Second)
	getHealth(h)
	if n := p.calls.Load(); n != 2 {
		t.Fatalf("pings = %d, want 2", n)
	}
}

// Exactly TTL old counts as expired.
func TestHealthCache_BoundaryExactlyTTLRefetches(t *testing.T) {
	p := &togglePinger{}
	h, clk := newCachedHealth(p)
	getHealth(h)
	clk.t = clk.t.Add(healthCacheTTL)
	getHealth(h)
	if n := p.calls.Load(); n != 2 {
		t.Fatalf("pings = %d, want 2", n)
	}
}

func TestHealthCache_UnhealthyCachedThenRecovers(t *testing.T) {
	p := &togglePinger{}
	p.fail.Store(true)
	h, clk := newCachedHealth(p)
	if rec := getHealth(h); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("first code = %d, want 503", rec.Code)
	}
	p.fail.Store(false)
	clk.t = clk.t.Add(time.Second)
	if rec := getHealth(h); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("cached code = %d, want 503", rec.Code)
	}
	if n := p.calls.Load(); n != 1 {
		t.Fatalf("pings = %d, want 1", n)
	}
	clk.t = clk.t.Add(healthCacheTTL)
	if rec := getHealth(h); rec.Code != http.StatusOK {
		t.Fatalf("recovered code = %d, want 200", rec.Code)
	}
}
