package qdrant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// collectionInfoServer serves GET /collections/{name} with the given vectors
// config and fails the test if a PUT (create) is attempted.
func collectionInfoServer(t *testing.T, vectors any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected %s %s: existing collection must not be recreated", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"status": "green",
				"config": map[string]any{"params": map[string]any{"vectors": vectors}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEnsureCollection_ExistingMatches(t *testing.T) {
	srv := collectionInfoServer(t, map[string]any{"size": 768, "distance": "Cosine"})
	if err := New(srv.URL).EnsureCollection(context.Background(), "col", 768); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureCollection_ExistingDimMismatch(t *testing.T) {
	srv := collectionInfoServer(t, map[string]any{"size": 384, "distance": "Cosine"})
	err := New(srv.URL).EnsureCollection(context.Background(), "col", 768)
	if err == nil {
		t.Fatal("expected error for dim mismatch")
	}
	for _, want := range []string{"col", "384", "768"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestEnsureCollection_ExistingDistanceMismatch(t *testing.T) {
	srv := collectionInfoServer(t, map[string]any{"size": 768, "distance": "Dot"})
	err := New(srv.URL).EnsureCollection(context.Background(), "col", 768)
	if err == nil || !strings.Contains(err.Error(), "Dot") {
		t.Fatalf("expected distance mismatch error mentioning Dot, got %v", err)
	}
}

func TestEnsureCollection_ExistingNamedVectorsRejected(t *testing.T) {
	srv := collectionInfoServer(t, map[string]any{
		"text": map[string]any{"size": 768, "distance": "Cosine"},
	})
	err := New(srv.URL).EnsureCollection(context.Background(), "col", 768)
	if err == nil || !strings.Contains(err.Error(), "named vectors") {
		t.Fatalf("expected named-vectors error, got %v", err)
	}
}

func TestEnsureCollection_ExistingMissingVectorsConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"name": "col"}})
	}))
	t.Cleanup(srv.Close)
	if err := New(srv.URL).EnsureCollection(context.Background(), "col", 768); err == nil {
		t.Fatal("expected error when vectors config is absent")
	}
}

func TestEnsureCollection_NonPositiveDim_NoRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	if err := New(srv.URL).EnsureCollection(context.Background(), "col", 0); err == nil {
		t.Fatal("expected error for dim=0")
	}
}
