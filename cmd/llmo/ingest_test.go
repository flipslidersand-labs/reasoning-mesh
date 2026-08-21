package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
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
		runIngest([]string{"--server", srv.URL, "--file", f.Name()})
	})

	if captured.CommitSHA != "file:"+f.Name() {
		t.Errorf("commit_sha = %q", captured.CommitSHA)
	}
	if captured.Diff == "" {
		t.Error("diff should contain file content")
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
	if payload.CommitSHA != "file:"+f.Name() {
		t.Errorf("commit_sha = %q", payload.CommitSHA)
	}
}
