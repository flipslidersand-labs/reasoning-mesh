package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
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
		runAsk([]string{"--server", srv.URL, "このエラーを調べて"})
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
		runAsk([]string{"--server", srv.URL, "debug", "this", "error"})
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
		runAsk([]string{"--server", srv.URL, "--token", "mytoken", "hello"})
	})
	if gotAuth != "Bearer mytoken" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestDefaultServerURL_EnvOverride(t *testing.T) {
	t.Setenv("LLMO_URL", "http://myhost:9999")
	if got := defaultServerURL(); got != "http://myhost:9999" {
		t.Errorf("got %q", got)
	}
}

func TestDefaultServerURL_Default(t *testing.T) {
	t.Setenv("LLMO_URL", "")
	if got := defaultServerURL(); got != "http://localhost:8080" {
		t.Errorf("got %q", got)
	}
}
