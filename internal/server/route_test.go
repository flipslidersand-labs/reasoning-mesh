package server_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/router"
	"github.com/flipslidersand/reasoning-mesh/internal/server"
)

func TestRoute_TruncatedFlag(t *testing.T) {
	for _, trunc := range []bool{true, false} {
		r := router.New(router.Config{Default: truncAdapter{truncated: trunc}})
		srv, pending := server.Build(server.Config{Router: r})
		t.Cleanup(pending.Stop)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/route", bytes.NewReader([]byte(`{"task":"hello"}`))))
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, body)
		}
		has := strings.Contains(body, `"truncated":true`)
		if has != trunc || (!trunc && strings.Contains(body, "truncated")) {
			t.Errorf("trunc=%v body=%s", trunc, body)
		}
	}
}
