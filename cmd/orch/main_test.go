package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/config"
	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/server"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func getHealth(t *testing.T, o, q server.Pinger) int {
	t.Helper()
	t.Setenv("LLMO_TRIGGER_TOKEN", "test-token")
	pending := server.NewPendingStore()
	t.Cleanup(pending.Stop)
	h := buildHandler(server.Config{Pending: pending}, o, q)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	return rec.Code
}

func TestBuildHandler_HealthReflectsDependencies(t *testing.T) {
	down := fakePinger{err: errors.New("down")}
	up := fakePinger{}
	if got := getHealth(t, up, up); got != http.StatusOK {
		t.Errorf("all up: got %d, want 200", got)
	}
	if got := getHealth(t, down, up); got != http.StatusServiceUnavailable {
		t.Errorf("ollama down: got %d, want 503", got)
	}
	if got := getHealth(t, up, down); got != http.StatusServiceUnavailable {
		t.Errorf("qdrant down: got %d, want 503", got)
	}
}

func TestQdrantTimeoutFor(t *testing.T) {
	tests := []struct {
		name string
		sec  int
		want time.Duration
	}{
		{"unset", 0, 30 * time.Second},
		{"negative", -5, 30 * time.Second},
		{"one", 1, time.Second},
		{"configured", 45, 45 * time.Second},
		{"max", config.MaxTimeoutSeconds, config.MaxTimeoutSeconds * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := qdrantTimeoutFor(tt.sec); got != tt.want {
				t.Errorf("qdrantTimeoutFor(%d) = %v, want %v", tt.sec, got, tt.want)
			}
		})
	}
}

func TestWriteTimeout(t *testing.T) {
	embed := knowledge.EmbedTimeout
	q30 := 30 * time.Second
	tests := []struct {
		name   string
		sec    int
		embed  time.Duration
		qdrant time.Duration
		want   time.Duration
	}{
		{"sum of ollama+embed+qdrant", 300, embed, q30, 360 * time.Second},
		{"qdrant timeout is reflected", 300, embed, 90 * time.Second, 420 * time.Second},
		{"embed timeout is reflected", 300, 50 * time.Second, q30, 380 * time.Second},
		{"floor for small ollama timeout", 1, embed, q30, 180 * time.Second},
		{"just below floor", 119, embed, q30, 180 * time.Second},
		{"exactly at floor", 120, embed, q30, 180 * time.Second},
		{"just above floor", 121, embed, q30, 181 * time.Second},
		{"max ollama and max qdrant do not overflow",
			config.MaxTimeoutSeconds, embed, config.MaxTimeoutSeconds * time.Second,
			time.Duration(2*config.MaxTimeoutSeconds)*time.Second + embed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := writeTimeout(tt.sec, tt.embed, tt.qdrant)
			if got != tt.want {
				t.Errorf("writeTimeout = %v, want %v", got, tt.want)
			}
			if got <= time.Duration(tt.sec)*time.Second {
				t.Errorf("writeTimeout %v must exceed ollama timeout %ds", got, tt.sec)
			}
		})
	}
}

func TestShutdownTimeout_EqualsWriteTimeout(t *testing.T) {
	for _, sec := range []int{1, 60, 120, 300, config.MaxTimeoutSeconds} {
		for _, q := range []time.Duration{qdrantTimeoutFor(0), qdrantTimeoutFor(120)} {
			got := shutdownTimeout(sec, knowledge.EmbedTimeout, q)
			want := writeTimeout(sec, knowledge.EmbedTimeout, q)
			if got != want {
				t.Errorf("sec=%d q=%v: shutdownTimeout = %v, want %v", sec, q, got, want)
			}
			if got <= 10*time.Second {
				t.Errorf("shutdownTimeout %v must exceed the old fixed 10s", got)
			}
		}
	}
}
