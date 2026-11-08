package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
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

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// slowPingerClock simulates a ping that runs until its timeout by advancing
// the injected clock by healthPingTimeout, then fails like a timed-out ping.
type slowPingerClock struct {
	calls atomic.Int32
	clk   *fakeClock
}

func (p *slowPingerClock) Ping(context.Context) error {
	p.calls.Add(1)
	p.clk.advance(healthPingTimeout)
	return errors.New("timeout")
}

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

// A ping that takes as long as the TTL must still leave a fresh cache: the TTL
// counts from when the result arrived, not from when the ping started.
func TestHealthCache_SlowPingResultIsFreshAfterwards(t *testing.T) {
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	p := &slowPingerClock{clk: clk}
	h := &healthHandler{cfg: HealthConfig{Ollama: p, Qdrant: okPinger{}}, now: clk.now}
	getHealth(h)
	rec := getHealth(h)
	if n := p.calls.Load(); n != 1 {
		t.Fatalf("pings = %d, want 1", n)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
}

// Concurrent requests share one round of pings and see the same result.
func TestHealthCache_ConcurrentRequestsShareOnePing(t *testing.T) {
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	p := &slowPingerClock{clk: clk}
	h := &healthHandler{cfg: HealthConfig{Ollama: p, Qdrant: okPinger{}}, now: clk.now}

	const n = 8
	start := make(chan struct{})
	recs := make([]*httptest.ResponseRecorder, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			recs[i] = getHealth(h)
		}()
	}
	close(start)
	wg.Wait()

	if got := p.calls.Load(); got != 1 {
		t.Fatalf("pings = %d, want 1", got)
	}
	for i, rec := range recs {
		if rec.Code != recs[0].Code || rec.Body.String() != recs[0].Body.String() {
			t.Fatalf("response %d differs: %d %q vs %d %q", i, rec.Code, rec.Body, recs[0].Code, recs[0].Body)
		}
	}
}

// A canceled first request must not poison the cache with a failure.
func TestHealthCache_CanceledRequestDoesNotCacheFailure(t *testing.T) {
	h := &healthHandler{cfg: HealthConfig{Ollama: okPinger{}, Qdrant: okPinger{}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil).WithContext(ctx))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if rec := getHealth(h); rec.Code != http.StatusOK {
		t.Fatalf("follow-up code = %d, want 200", rec.Code)
	}
}
