package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/config"
)

// captureStdout redirects os.Stdout to a pipe and returns what was written.
func captureOutput(fn func()) string {
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = orig
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

func TestRunAsk_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/route" || r.Method != http.MethodPost {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(askResponse{Answer: "resolved!", Model: "stub"})
	}))
	defer srv.Close()

	out := captureOutput(func() {
		if err := runAsk(nil, []string{"--server", srv.URL, "このエラーを調べて"}); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	if out != "resolved!\n" {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestRunAsk_MultiWordTask(t *testing.T) {
	var captured askRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(askResponse{Answer: "ok", Model: "stub"})
	}))
	defer srv.Close()

	captureOutput(func() {
		_ = runAsk(nil, []string{"--server", srv.URL, "debug", "this", "error"})
	})
	if captured.Task != "debug this error" {
		t.Errorf("task = %q", captured.Task)
	}
}

func TestRunAsk_BearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(askResponse{Answer: "ok", Model: "stub"})
	}))
	defer srv.Close()

	captureOutput(func() {
		_ = runAsk(nil, []string{"--server", srv.URL, "--token", "mytoken", "hello"})
	})
	if gotAuth != "Bearer mytoken" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestRunAsk_ReturnsError_OnServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := runAsk(nil, []string{"--server", srv.URL, "hello"})
	if err == nil {
		t.Error("expected error for non-200 response")
	}
}

func TestDefaultServerURL_FromConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Host = "myhost"
	cfg.Server.Port = 9090
	if got := defaultServerURL(cfg); got != "http://myhost:9090" {
		t.Errorf("got %q", got)
	}
}

func TestDefaultServerURL_FromConfigDefaultHost(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Port = 8080
	if got := defaultServerURL(cfg); got != "http://localhost:8080" {
		t.Errorf("got %q", got)
	}
}

func TestDefaultServerURL_EnvOverride(t *testing.T) {
	t.Setenv("LLMO_URL", "http://myhost:9999")
	if got := defaultServerURL(nil); got != "http://myhost:9999" {
		t.Errorf("got %q", got)
	}
}

func TestDefaultServerURL_Default(t *testing.T) {
	t.Setenv("LLMO_URL", "")
	if got := defaultServerURL(nil); got != "http://localhost:8080" {
		t.Errorf("got %q", got)
	}
}

func TestRunAsk_UsesOrchToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(askResponse{Answer: "ok", Model: "stub"})
	}))
	defer srv.Close()

	t.Setenv("LLMO_ORCH_TOKEN", "orch-secret")
	captureOutput(func() {
		_ = runAsk(nil, []string{"--server", srv.URL, "hello"})
	})
	if gotAuth != "Bearer orch-secret" {
		t.Errorf("Authorization = %q, want Bearer orch-secret", gotAuth)
	}
}
