package server_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/router"
	"github.com/flipslidersand/reasoning-mesh/internal/server"
)

const upstreamLeak = "dial tcp 127.0.0.1:11434: connect: connection refused"

type failingAdapter struct{}

func (failingAdapter) Name() string { return "failing" }
func (failingAdapter) Generate(_ context.Context, _ string) (router.Response, error) {
	return router.Response{}, errors.New(upstreamLeak)
}

func buildFailingServer(t *testing.T) http.Handler {
	t.Helper()
	srv, pending := server.Build(server.Config{
		Router: router.New(router.Config{Default: failingAdapter{}}),
	})
	t.Cleanup(pending.Stop)
	return srv
}

func TestUpstreamError_NotLeaked(t *testing.T) {
	for _, tc := range []struct{ path, body string }{
		{"/v1/route", `{"task":"hello"}`},
		{"/v1/infer", `{"prompt":"hello"}`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			buildFailingServer(t).ServeHTTP(rec, req)

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status %d, want 500", rec.Code)
			}
			if strings.Contains(rec.Body.String(), "127.0.0.1") || strings.Contains(rec.Body.String(), "11434") {
				t.Errorf("response leaks upstream detail: %s", rec.Body.String())
			}
		})
	}
}
