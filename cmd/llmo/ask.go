package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/config"
)

type askRequest struct {
	Task string `json:"task"`
}

type askResponse struct {
	Answer string `json:"answer"`
	Model  string `json:"model"`
	Error  string `json:"error,omitempty"`
}

func runAsk(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("ask", flag.ExitOnError)
	serverURL := fs.String("server", defaultServerURL(cfg), "llmo server base URL")
	token := fs.String("token", os.Getenv("LLMO_ORCH_TOKEN"), "bearer token")
	_ = fs.Parse(args)

	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: llmo ask [flags] \"<task>\"")
		return fmt.Errorf("no task provided")
	}
	task := strings.Join(fs.Args(), " ")

	body, err := json.Marshal(askRequest{Task: task})
	if err != nil {
		return fmt.Errorf("ask: marshal request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, *serverURL+"/v1/route", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ask: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if *token != "" {
		req.Header.Set("Authorization", "Bearer "+*token)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ask: request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ask: server returned %d: %s", resp.StatusCode, raw)
	}

	var ar askResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		return fmt.Errorf("ask: decode response: %w", err)
	}
	if ar.Error != "" {
		return fmt.Errorf("ask: %s", ar.Error)
	}
	fmt.Println(ar.Answer)
	return nil
}

// defaultServerURL returns the server URL from config, falling back to
// the LLMO_URL environment variable, then http://localhost:8080.
func defaultServerURL(cfg *config.Config) string {
	if cfg != nil && cfg.Server.Port != 0 {
		host := cfg.Server.Host
		if host == "" {
			host = "localhost"
		}
		return fmt.Sprintf("http://%s:%d", host, cfg.Server.Port)
	}
	if v := os.Getenv("LLMO_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://localhost:8080"
}
