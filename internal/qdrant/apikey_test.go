package qdrant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// headerRecorder serves a minimal Qdrant and records the api-key of every request.
func headerRecorder(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		keys = append(keys, r.Header.Get("api-key"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":[],"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), keys...)
	}
}

// #226: with an API key configured, every request kind carries the api-key header.
func TestWithAPIKey_SentOnAllRequests(t *testing.T) {
	srv, keys := headerRecorder(t)
	c := New(srv.URL).WithAPIKey("s3cret")
	ctx := context.Background()

	_ = c.Ping(ctx)
	_, _ = c.Search(ctx, "col", []float32{1}, 1, nil)
	_ = c.Upsert(ctx, "col", "id1", []float32{1}, map[string]any{"a": 1})
	_ = c.UpdatePayload(ctx, "col", "id1", map[string]any{"a": 2})
	_, _ = c.GetByIDs(ctx, "col", []string{"id1"})

	got := keys()
	if len(got) < 5 {
		t.Fatalf("recorded %d requests, want >= 5", len(got))
	}
	for i, k := range got {
		if k != "s3cret" {
			t.Errorf("request %d api-key = %q, want s3cret", i, k)
		}
	}
}

// Without a key (current unauthenticated deployments) no header is sent.
func TestNoAPIKey_NoHeader(t *testing.T) {
	srv, keys := headerRecorder(t)
	c := New(srv.URL)
	_ = c.Ping(context.Background())
	for _, k := range keys() {
		if k != "" {
			t.Errorf("api-key header %q sent without a configured key", k)
		}
	}
}
