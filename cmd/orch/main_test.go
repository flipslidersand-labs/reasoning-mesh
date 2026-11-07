package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/server"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func getHealth(t *testing.T, o, q server.Pinger) int {
	t.Helper()
	t.Setenv("LLMO_TRIGGER_TOKEN", "test-token")
	h := buildHandler(server.Config{}, o, q)
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

func TestWriteTimeout_ExceedsOllamaTimeout(t *testing.T) {
	for _, sec := range []int{60, 180, 300, 900} {
		got := writeTimeout(sec)
		if got <= time.Duration(sec)*time.Second {
			t.Errorf("timeout_seconds=%d: WriteTimeout %v must exceed ollama timeout", sec, got)
		}
	}
	if got := writeTimeout(1); got < 180*time.Second {
		t.Errorf("WriteTimeout %v must not drop below the 180s floor", got)
	}
}
