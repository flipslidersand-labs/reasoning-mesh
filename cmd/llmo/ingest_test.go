package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/validate"
)

func TestRunIngest_File(t *testing.T) {
	// Write a temporary file to ingest
	f, err := os.CreateTemp(t.TempDir(), "doc*.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("# Test Document\nsome content")
	f.Close()

	var captured ingestRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/trigger" || r.Method != http.MethodPost {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(ingestResponse{Status: "accepted", CommitSHA: captured.CommitSHA})
	}))
	defer srv.Close()

	captureOutput(func() {
		if err := runIngest(nil, []string{"--server", srv.URL, "--file", f.Name()}); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	if err := validate.CommitSHA(captured.CommitSHA); err != nil {
		t.Errorf("commit_sha = %q rejected by server validation: %v", captured.CommitSHA, err)
	}
	if captured.Diff == "" {
		t.Error("diff should contain file content")
	}
}

func TestRunIngest_ReturnsError_OnServerError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "doc*.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("content")
	f.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	err = runIngest(nil, []string{"--server", srv.URL, "--file", f.Name()})
	if err == nil {
		t.Error("expected error for non-202 response")
	}
}

func TestRunIngest_FileMissing(t *testing.T) {
	// Use a fake server that should never be called
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called for missing file")
	}))
	defer srv.Close()

	// buildIngestPayload with a missing file should return an error
	_, err := buildIngestPayload("", "/nonexistent/file.md")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestBuildIngestPayload_FileTooLarge(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "big*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if err := f.Truncate(maxIngestFileBytes + 1); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	_, err = buildIngestPayload("", f.Name())
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
}

func TestBuildIngestPayload_FileContent(t *testing.T) {
	f, _ := os.CreateTemp(t.TempDir(), "doc*.md")
	_, _ = f.WriteString("hello world")
	f.Close()

	payload, err := buildIngestPayload("", f.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payload.Diff != "hello world" {
		t.Errorf("diff = %q", payload.Diff)
	}
	if err := validate.CommitSHA(payload.CommitSHA); err != nil {
		t.Errorf("commit_sha = %q rejected by server validation: %v", payload.CommitSHA, err)
	}
	// sha256("hello world") first 40 hex chars: deterministic from content.
	if want := "b94d27b9934d3e08a52e52d7da7dabfac484efe3"; payload.CommitSHA != want {
		t.Errorf("commit_sha = %q, want %q", payload.CommitSHA, want)
	}
}

func TestRunIngest_ServerFlagTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	f := filepath.Join(t.TempDir(), "d.md")
	if err := os.WriteFile(f, []byte("# x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	captureOutput(func() {
		_ = runIngest(nil, []string{"--server", srv.URL + "/", "--file", f})
	})
	if gotPath != "/v1/trigger" {
		t.Errorf("path = %q, want /v1/trigger", gotPath)
	}
}
