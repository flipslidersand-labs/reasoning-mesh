package eval_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
)

type capturedFeedback struct {
	KnowledgeIDs []string `json:"knowledge_ids"`
	Outcome      bool     `json:"outcome"`
	Evaluator    string   `json:"evaluator"`
}

func TestHTTPFeedbackSender_Send(t *testing.T) {
	var got capturedFeedback
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/feedback" || r.Method != http.MethodPost {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "accepted"})
	}))
	defer srv.Close()

	sender := eval.NewHTTPFeedbackSender(srv.URL, "")
	err := sender.Send(context.Background(), []string{"id-001", "id-002"}, true, "eval")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(got.KnowledgeIDs) != 2 {
		t.Errorf("knowledge_ids = %v", got.KnowledgeIDs)
	}
	if !got.Outcome {
		t.Error("outcome should be true")
	}
	if got.Evaluator != "eval" {
		t.Errorf("evaluator = %q", got.Evaluator)
	}
}

func TestHTTPFeedbackSender_EmptyIDs(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	sender := eval.NewHTTPFeedbackSender(srv.URL, "")
	// Empty IDs → no-op, no HTTP call
	err := sender.Send(context.Background(), nil, true, "eval")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("server should not be called for empty knowledge_ids")
	}
}

func TestHTTPFeedbackSender_BearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "accepted"})
	}))
	defer srv.Close()

	sender := eval.NewHTTPFeedbackSender(srv.URL, "mytoken")
	_ = sender.Send(context.Background(), []string{"id-001"}, true, "ci")
	if gotAuth != "Bearer mytoken" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestHTTPFeedbackSender_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	sender := eval.NewHTTPFeedbackSender(srv.URL, "")
	err := sender.Send(context.Background(), []string{"id-001"}, false, "eval")
	if err == nil {
		t.Error("expected error on 500 response")
	}
}
