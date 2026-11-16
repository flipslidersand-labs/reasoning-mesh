package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/flipslidersand-labs/reasoning-mesh/internal/config"
)

// maxIngestFileBytes is the maximum size accepted for --file ingestion.
// It matches the server's maxTriggerBodyBytes limit for /v1/trigger, so
// oversized files are rejected before being read into memory.
const maxIngestFileBytes = 16 << 20 // 16 MiB

type ingestRequest struct {
	CommitSHA string `json:"commit_sha"`
	Diff      string `json:"diff,omitempty"`
	CILog     string `json:"ci_log,omitempty"`
}

type ingestResponse struct {
	Status    string `json:"status"`
	CommitSHA string `json:"commit_sha"`
	Error     string `json:"error,omitempty"`
}

// ingestDefaultToken returns the bearer token for /v1/trigger: LLMO_TRIGGER_TOKEN,
// falling back to LLMO_ORCH_TOKEN for backward compatibility.
func ingestDefaultToken() string {
	if t := os.Getenv("LLMO_TRIGGER_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("LLMO_ORCH_TOKEN")
}

func runIngest(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	serverURL := fs.String("server", defaultServerURL(cfg), "llmo server base URL")
	token := fs.String("token", ingestDefaultToken(), "bearer token (default: $LLMO_TRIGGER_TOKEN, falling back to $LLMO_ORCH_TOKEN)")
	commitRef := fs.String("commit", "", "git commit ref to ingest (e.g. HEAD)")
	filePath := fs.String("file", "", "markdown/text file to ingest as diff content")
	_ = fs.Parse(args)

	if *commitRef == "" && *filePath == "" {
		fmt.Fprintln(os.Stderr, "usage: llmo ingest --commit <ref>|--file <path>")
		return fmt.Errorf("--commit or --file is required")
	}

	payload, err := buildIngestPayload(*commitRef, *filePath)
	if err != nil {
		return fmt.Errorf("ingest: build payload: %w", err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("ingest: marshal payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, trimServerURL(*serverURL)+"/v1/trigger", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ingest: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if *token != "" {
		req.Header.Set("Authorization", "Bearer "+*token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ingest: request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("ingest: server returned %d: %s", resp.StatusCode, raw)
	}

	var ir ingestResponse
	if err := json.Unmarshal(raw, &ir); err != nil {
		return fmt.Errorf("ingest: decode response: %w", err)
	}
	fmt.Printf("accepted: %s\n", ir.CommitSHA)
	return nil
}

func buildIngestPayload(commitRef, filePath string) (ingestRequest, error) {
	if filePath != "" {
		info, err := os.Stat(filePath)
		if err != nil {
			return ingestRequest{}, fmt.Errorf("stat file %s: %w", filePath, err)
		}
		if info.Size() > maxIngestFileBytes {
			return ingestRequest{}, fmt.Errorf("file %s is %d bytes, exceeds max allowed size of %d bytes", filePath, info.Size(), maxIngestFileBytes)
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			return ingestRequest{}, fmt.Errorf("read file %s: %w", filePath, err)
		}
		// Server requires commit_sha to be 7-64 lowercase hex; derive a
		// deterministic one from the content.
		sum := sha256.Sum256(content)
		sha := hex.EncodeToString(sum[:])[:40]
		return ingestRequest{CommitSHA: sha, Diff: string(content)}, nil
	}

	// Resolve commit SHA
	shaOut, err := exec.Command("git", "rev-parse", commitRef).Output()
	if err != nil {
		return ingestRequest{}, fmt.Errorf("git rev-parse %s: %w", commitRef, err)
	}
	sha := strings.TrimSpace(string(shaOut))

	// A root commit has no parent, so "git diff <ref>~1" would fail; an empty
	// diff is explicitly accepted in that case. Any other failure is an error.
	commitObj, err := exec.Command("git", "cat-file", "commit", sha).Output()
	if err != nil {
		return ingestRequest{}, fmt.Errorf("git cat-file %s: %w", commitRef, err)
	}
	diff := ""
	if hasParent(string(commitObj)) {
		// Cap at 32 KiB to match CI workflow.
		diffOut, err := exec.Command("git", "diff", commitRef+"~1", commitRef, "--", "*.go").Output()
		if err != nil {
			return ingestRequest{}, fmt.Errorf("git diff %s: %w", commitRef, err)
		}
		diff = truncateUTF8(string(diffOut), 32*1024)
	}

	return ingestRequest{CommitSHA: sha, Diff: diff}, nil
}

// truncateUTF8 cuts s to at most max bytes without splitting a UTF-8 rune.
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

// hasParent reports whether a raw commit object lists a parent in its header.
func hasParent(commitObj string) bool {
	header, _, _ := strings.Cut(commitObj, "\n\n")
	for _, l := range strings.Split(header, "\n") {
		if strings.HasPrefix(l, "parent ") {
			return true
		}
	}
	return false
}
