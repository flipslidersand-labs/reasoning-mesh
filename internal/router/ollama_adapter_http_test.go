package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestOllamaAdapter_TextTokensModel(t *testing.T) {
	var gotPath, gotModel, gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var req struct{ Model, Prompt string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotModel, gotPrompt = req.Model, req.Prompt
		_, _ = w.Write([]byte(`{"model":"resp-model","response":"hello","done":true,"done_reason":"stop","prompt_eval_count":11,"eval_count":5}`))
	}))
	defer srv.Close()
	a := NewOllamaAdapter(ollama.New(srv.URL, 5), "req-model")
	r, err := a.Generate(context.Background(), "the prompt")
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "hello" || r.PromptTokens != 11 || r.TotalTokens != 16 || r.Model != "resp-model" || r.Truncated {
		t.Fatalf("got %+v", r)
	}
	if gotPath != "/api/generate" || gotModel != "req-model" || gotPrompt != "the prompt" {
		t.Fatalf("request: path=%q model=%q prompt=%q", gotPath, gotModel, gotPrompt)
	}
}

func TestOllamaAdapter_UpstreamErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	a := NewOllamaAdapter(ollama.New(srv.URL, 5), "m")
	r, err := a.Generate(context.Background(), "p")
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("got %v", err)
	}
	if r != (Response{}) {
		t.Fatalf("want zero Response on error, got %+v", r)
	}
}

func TestOllamaAdapter_DecodeErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{bad`))
	}))
	defer srv.Close()
	a := NewOllamaAdapter(ollama.New(srv.URL, 5), "m")
	if _, err := a.Generate(context.Background(), "p"); err == nil {
		t.Fatal("want decode error")
	}
}
