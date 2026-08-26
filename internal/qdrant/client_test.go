package qdrant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func qdrantOK(t *testing.T, method, pathSuffix string, respBody any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(respBody)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func qdrantError(t *testing.T, code int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// --- Ping ---

func TestPing_OK(t *testing.T) {
	srv := qdrantOK(t, http.MethodGet, "/healthz", map[string]string{"status": "ok"})
	c := New(srv.URL)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPing_NonOK(t *testing.T) {
	srv := qdrantError(t, http.StatusServiceUnavailable)
	c := New(srv.URL)
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("expected error for 503")
	}
}

func TestPing_Unreachable(t *testing.T) {
	c := New("http://127.0.0.1:19999")
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

// --- Upsert ---

func TestUpsert_OK(t *testing.T) {
	srv := qdrantOK(t, http.MethodPut, "/collections/col/points", map[string]string{"result": "ok"})
	c := New(srv.URL)
	err := c.Upsert(context.Background(), "col", "550e8400-e29b-41d4-a716-446655440000",
		[]float32{0.1, 0.2, 0.3}, map[string]any{"content": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpsert_NonOK(t *testing.T) {
	srv := qdrantError(t, http.StatusBadRequest)
	c := New(srv.URL)
	err := c.Upsert(context.Background(), "col", "id", []float32{0.1}, nil)
	if err == nil {
		t.Fatal("expected error for 400")
	}
}

// --- BulkUpsert ---

func TestBulkUpsert_OK(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer srv.Close()
	c := New(srv.URL)

	pts := []UpsertPoint{
		{ID: "id-1", Vector: []float32{0.1, 0.2}, Payload: map[string]any{"k": "v1"}},
		{ID: "id-2", Vector: []float32{0.3, 0.4}, Payload: map[string]any{"k": "v2"}},
	}
	if err := c.BulkUpsert(context.Background(), "col", pts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	points, _ := capturedBody["points"].([]any)
	if len(points) != 2 {
		t.Errorf("expected 2 points in request, got %d", len(points))
	}
}

func TestBulkUpsert_Empty_IsNoop(t *testing.T) {
	// No server needed — empty slice must return nil without making any HTTP call.
	c := New("http://127.0.0.1:19998")
	if err := c.BulkUpsert(context.Background(), "col", nil); err != nil {
		t.Fatalf("empty BulkUpsert should be a no-op, got: %v", err)
	}
}

func TestBulkUpsert_NonOK(t *testing.T) {
	srv := qdrantError(t, http.StatusUnprocessableEntity)
	c := New(srv.URL)
	pts := []UpsertPoint{{ID: "id-1", Vector: []float32{0.1}, Payload: nil}}
	if err := c.BulkUpsert(context.Background(), "col", pts); err == nil {
		t.Fatal("expected error for 422")
	}
}

// --- Search ---

func TestSearch_WithResults(t *testing.T) {
	resp := map[string]any{
		"result": []map[string]any{
			{"id": "id-1", "score": 0.95, "payload": map[string]any{"content": "chunk1"}},
			{"id": "id-2", "score": 0.80, "payload": map[string]any{"content": "chunk2"}},
		},
	}
	srv := qdrantOK(t, http.MethodPost, "/collections/col/points/search", resp)
	c := New(srv.URL)

	results, err := c.Search(context.Background(), "col", []float32{0.1, 0.2}, 5, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "id-1" {
		t.Errorf("first result ID = %q, want id-1", results[0].ID)
	}
	if results[0].Score < 0.94 {
		t.Errorf("first result score = %.2f, want ~0.95", results[0].Score)
	}
}

func TestSearch_EmptyResults(t *testing.T) {
	resp := map[string]any{"result": []any{}}
	srv := qdrantOK(t, http.MethodPost, "/collections/col/points/search", resp)
	c := New(srv.URL)

	results, err := c.Search(context.Background(), "col", []float32{0.1}, 5, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearch_NonOK(t *testing.T) {
	srv := qdrantError(t, http.StatusInternalServerError)
	c := New(srv.URL)
	_, err := c.Search(context.Background(), "col", []float32{0.1}, 5, nil)
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestSearch_WithFilter(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": []any{}})
	}))
	defer srv.Close()
	c := New(srv.URL)

	filter := map[string]any{"must": []map[string]any{{"key": "task_type", "match": map[string]any{"value": "debugging"}}}}
	_, _ = c.Search(context.Background(), "col", []float32{0.1}, 3, filter)

	if capturedBody["filter"] == nil {
		t.Error("expected filter to be included in request body")
	}
}

// --- GetByID ---

func TestGetByID_OK(t *testing.T) {
	resp := map[string]any{
		"result": []map[string]any{
			{"id": "abc", "score": 0, "payload": map[string]any{"content": "some knowledge"}},
		},
	}
	srv := qdrantOK(t, http.MethodPost, "/collections/col/points", resp)
	c := New(srv.URL)

	results, err := c.GetByID(context.Background(), "col", "abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].ID != "abc" {
		t.Errorf("unexpected results: %+v", results)
	}
}

func TestGetByID_NonOK(t *testing.T) {
	srv := qdrantError(t, http.StatusNotFound)
	c := New(srv.URL)
	_, err := c.GetByID(context.Background(), "col", "missing")
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

// --- UpdatePayload ---

func TestUpdatePayload_OK(t *testing.T) {
	srv := qdrantOK(t, http.MethodPost, "/collections/col/points/payload", map[string]string{"result": "ok"})
	c := New(srv.URL)
	err := c.UpdatePayload(context.Background(), "col", "id-1", map[string]any{"usage_count": 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdatePayload_NonOK(t *testing.T) {
	srv := qdrantError(t, http.StatusUnprocessableEntity)
	c := New(srv.URL)
	err := c.UpdatePayload(context.Background(), "col", "id-1", map[string]any{"x": 1})
	if err == nil {
		t.Fatal("expected error for 422")
	}
}

// --- EnsureCollection ---

func TestEnsureCollection_AlreadyExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"name": "col"}})
			return
		}
		// PUT should not be called
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := New(srv.URL)

	if err := c.EnsureCollection(context.Background(), "col", 384); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureCollection_Creates(t *testing.T) {
	var putCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodPut {
			putCalled = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer srv.Close()
	c := New(srv.URL)

	if err := c.EnsureCollection(context.Background(), "col", 384); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !putCalled {
		t.Error("expected PUT /collections/col to be called")
	}
}

func TestEnsureCollection_UnexpectedStatus(t *testing.T) {
	srv := qdrantError(t, http.StatusForbidden)
	c := New(srv.URL)
	if err := c.EnsureCollection(context.Background(), "col", 384); err == nil {
		t.Fatal("expected error for 403")
	}
}

func TestUpdatePayload_SendsPointsAndPayload(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer srv.Close()
	c := New(srv.URL)

	_ = c.UpdatePayload(context.Background(), "col", "my-id", map[string]any{"score": 0.9})

	if body["payload"] == nil {
		t.Error("expected 'payload' in request body")
	}
	points, _ := body["points"].([]any)
	if len(points) != 1 || points[0].(string) != "my-id" {
		t.Errorf("expected points=[my-id], got %v", body["points"])
	}
}
