package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/ollama"
)

func TestOllamaAdapter_TruncatedFlag(t *testing.T) {
	for reason, want := range map[string]bool{"length": true, "stop": false} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"model":"m","response":"x","done":true,"done_reason":"` + reason + `"}`))
		}))
		a := NewOllamaAdapter(ollama.New(srv.URL, 5), "m")
		r, err := a.Generate(context.Background(), "p")
		srv.Close()
		if err != nil || r.Truncated != want {
			t.Fatalf("reason=%s: got %+v, %v", reason, r, err)
		}
	}
}
