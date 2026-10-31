package qdrant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBulkUpsert_InvalidCollection_NoRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := New(srv.URL)
	pts := []UpsertPoint{{ID: "id-1", Vector: []float32{0.1}, Payload: map[string]any{}}}

	names := map[string]string{
		"empty":     "",
		"dotdot":    "..",
		"slash":     "a/b",
		"traversal": "../admin",
		"query":     "col?x=1",
		"fragment":  "col#frag",
		"too long":  strings.Repeat("a", 65),
		"newline":   "col\nname",
		"nul":       "col\x00name",
		"tab":       "col\tname",
		"space":     "col name",
		"percent":   "col%2Fname",
		"backslash": `col\name`,
		"non-ascii": "コレクション",
	}
	for label, name := range names {
		t.Run(label, func(t *testing.T) {
			if err := c.BulkUpsert(context.Background(), name, pts); err == nil {
				t.Fatalf("expected error for collection %q", name)
			}
		})
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("server received %d request(s); invalid names must fail before any HTTP call", n)
	}
}
