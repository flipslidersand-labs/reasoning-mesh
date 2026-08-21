package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type askRequest struct {
	Task string `json:"task"`
}

type askResponse struct {
	Answer string `json:"answer"`
	Model  string `json:"model"`
	Error  string `json:"error,omitempty"`
}

func runAsk(args []string) {
	fs := flag.NewFlagSet("ask", flag.ExitOnError)
	serverURL := fs.String("server", defaultServerURL(), "llmo server base URL")
	token := fs.String("token", os.Getenv("LLMO_TRIGGER_TOKEN"), "bearer token")
	_ = fs.Parse(args)

	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: llmo ask [flags] \"<task>\"")
		os.Exit(1)
	}
	task := strings.Join(fs.Args(), " ")

	body, _ := json.Marshal(askRequest{Task: task})
	req, err := http.NewRequest(http.MethodPost, *serverURL+"/v1/route", bytes.NewReader(body))
	if err != nil {
		log.Fatalf("ask: build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if *token != "" {
		req.Header.Set("Authorization", "Bearer "+*token)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("ask: request failed: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("ask: server returned %d: %s", resp.StatusCode, raw)
	}

	var ar askResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		log.Fatalf("ask: decode response: %v", err)
	}
	if ar.Error != "" {
		fmt.Fprintf(os.Stderr, "error: %s\n", ar.Error)
		os.Exit(1)
	}
	fmt.Println(ar.Answer)
}

func defaultServerURL() string {
	if v := os.Getenv("LLMO_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://localhost:8080"
}
