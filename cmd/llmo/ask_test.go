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

func TestDefaultServerURL_Table(t *testing.T) {
	mk := func(host string, port int) *config.Config {
		c := &config.Config{}
		c.Server.Host = host
		c.Server.Port = port
		return c
	}
	tests := []struct {
		name string
		env  string
		cfg  *config.Config
		want string
	}{
		{"env beats config", "http://remote:1234", mk("myhost", 9090), "http://remote:1234"},
		{"env trailing slash trimmed", "http://remote:1234//", nil, "http://remote:1234"},
		{"config used without env", "", mk("myhost", 9090), "http://myhost:9090"},
		{"config 0.0.0.0 -> localhost", "", mk("0.0.0.0", 9090), "http://localhost:9090"},
		{"config empty host -> localhost", "", mk("", 9090), "http://localhost:9090"},
		{"nothing set", "", nil, "http://localhost:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LLMO_URL", tt.env)
			if got := defaultServerURL(tt.cfg); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunAsk_ServerFlagTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(askResponse{Answer: "ok", Model: "stub"})
	}))
	defer srv.Close()

	captureOutput(func() {
		_ = runAsk(nil, []string{"--server", srv.URL + "/", "hello"})
	})
	if gotPath != "/v1/route" {
		t.Errorf("path = %q, want /v1/route", gotPath)
	}
}
