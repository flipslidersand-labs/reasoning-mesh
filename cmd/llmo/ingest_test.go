package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	if !strings.Contains(err.Error(), "exceeds max allowed size") {
		t.Errorf("error = %v, want size-limit error", err)
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

func TestBuildIngestPayload_FileMissing(t *testing.T) {
	_, err := buildIngestPayload("", filepath.Join(t.TempDir(), "nope.md"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "stat file") {
		t.Errorf("error = %v, want it to mention stat file", err)
	}
}

func TestBuildIngestPayload_FileAtLimit(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "limit*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(maxIngestFileBytes); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	payload, err := buildIngestPayload("", f.Name())
	if err != nil {
		t.Fatalf("file of exactly maxIngestFileBytes must be accepted: %v", err)
	}
	if len(payload.Diff) != maxIngestFileBytes {
		t.Errorf("len(diff) = %d, want %d", len(payload.Diff), maxIngestFileBytes)
	}
}

// isolatedGitEnv returns the environment with git repo-selection variables
// removed so test git commands can never touch an outer repository.
func isolatedGitEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "GIT_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
}

// setupGitRepo creates a repo in a temp dir, chdirs into it (restored on
// cleanup), and strips GIT_* variables from the process env so that the
// production code's git calls resolve against this repo only.
func setupGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "GIT_") {
			t.Setenv(k, "") // testing restores the original value on cleanup
			os.Unsetenv(k)
		}
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Chdir(dir)
	runGit(t, dir, "init", "-q")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = isolatedGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", name)
	runGit(t, dir, "commit", "-q", "-m", "c")
	return runGit(t, dir, "rev-parse", "HEAD")
}

func TestBuildIngestPayload_GitRevParseFails(t *testing.T) {
	setupGitRepo(t)
	_, err := buildIngestPayload("no-such-ref", "")
	if err == nil || !strings.Contains(err.Error(), "git rev-parse") {
		t.Errorf("error = %v, want git rev-parse error", err)
	}
}

func TestBuildIngestPayload_GitCommit(t *testing.T) {
	dir := setupGitRepo(t)
	commitFile(t, dir, "a.go", "package a\n")
	commitFile(t, dir, "README.md", "# readme\n")
	// Change a non-.go file in the same commit; it must be excluded from the diff.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# readme\nmd-only-change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "README.md")
	sha := commitFile(t, dir, "a.go", "package a\n\nvar X = 1\n")

	payload, err := buildIngestPayload("HEAD", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payload.CommitSHA != sha {
		t.Errorf("commit_sha = %q, want %q", payload.CommitSHA, sha)
	}
	if !strings.Contains(payload.Diff, "+var X = 1") {
		t.Errorf("diff missing change: %q", payload.Diff)
	}
	if strings.Contains(payload.Diff, "README.md") || strings.Contains(payload.Diff, "md-only-change") {
		t.Errorf("diff must be limited to *.go files: %q", payload.Diff)
	}
}

// A root commit has no parent, so the diff command fails; the error is
// ignored and the payload has an empty diff.
func TestBuildIngestPayload_GitRootCommitEmptyDiff(t *testing.T) {
	dir := setupGitRepo(t)
	sha := commitFile(t, dir, "a.go", "package a\n")

	payload, err := buildIngestPayload("HEAD", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payload.CommitSHA != sha || payload.Diff != "" {
		t.Errorf("payload = {%q, len(diff)=%d}, want {%q, empty diff}", payload.CommitSHA, len(payload.Diff), sha)
	}
}

func TestBuildIngestPayload_GitDiffTruncatedTo32KiB(t *testing.T) {
	dir := setupGitRepo(t)
	commitFile(t, dir, "a.go", "package a\n")
	big := "package a\n" + strings.Repeat("// "+strings.Repeat("x", 77)+"\n", 1000) // ~80KB
	commitFile(t, dir, "a.go", big)

	payload, err := buildIngestPayload("HEAD", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(payload.Diff) != 32*1024 {
		t.Errorf("len(diff) = %d, want %d", len(payload.Diff), 32*1024)
	}
}
