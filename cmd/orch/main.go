package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/flipslidersand/reasoning-mesh/internal/config"
	"github.com/flipslidersand/reasoning-mesh/internal/ollama"
	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
)

func main() {
	cfgPath := flag.String("config", "config/config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(cfg *config.Config) error {
	ctx := context.Background()

	ollamaClient := ollama.New(cfg.Ollama.Endpoint, cfg.Ollama.TimeoutSeconds)
	if err := ollamaClient.Ping(ctx); err != nil {
		return fmt.Errorf("ollama unreachable: %w", err)
	}
	log.Printf("ollama OK (%s)", cfg.Ollama.Endpoint)

	qdrantClient := qdrant.New(cfg.Qdrant.Endpoint)
	if err := qdrantClient.Ping(ctx); err != nil {
		return fmt.Errorf("qdrant unreachable: %w", err)
	}
	log.Printf("qdrant OK (%s)", cfg.Qdrant.Endpoint)

	log.Println("reasoning-mesh ready")
	return nil
}
