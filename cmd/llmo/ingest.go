package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/config"
)

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

func runIngest(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	serverURL := fs.String("server", defaultServerURL(cfg), "llmo server base URL")
	token := fs.String("token", os.Getenv("LLMO_ORCH_TOKEN"), "bearer token")
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
	req, err := http.NewRequest(http.MethodPost, *serverURL+"/v1/trigger", bytes.NewReader(body))
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
		content, err := os.ReadFile(filePath)
		if err != nil {
			return ingestRequest{}, fmt.Errorf("read file %s: %w", filePath, err)
		}
		sha := "file:" + filePath
		return ingestRequest{CommitSHA: sha, Diff: string(content)}, nil
	}

	// Resolve commit SHA
	shaOut, err := exec.Command("git", "rev-parse", commitRef).Output()
	if err != nil {
		return ingestRequest{}, fmt.Errorf("git rev-parse %s: %w", commitRef, err)
	}
	sha := strings.TrimSpace(string(shaOut))

	// Get diff (cap at 32 KiB to match CI workflow); ignore error — empty diff is acceptable
	diffOut, _ := exec.Command("git", "diff", commitRef+"~1", commitRef, "--", "*.go").Output()
	diff := string(diffOut)
	if len(diff) > 32*1024 {
		diff = diff[:32*1024]
	}

	return ingestRequest{CommitSHA: sha, Diff: diff}, nil
}
