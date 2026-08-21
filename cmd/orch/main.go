package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/config"
	"github.com/flipslidersand/reasoning-mesh/internal/ollama"
	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
	"github.com/flipslidersand/reasoning-mesh/internal/router"
	"github.com/flipslidersand/reasoning-mesh/internal/server"
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
		log.Printf("WARN: ollama unreachable (%v) — route endpoint will return errors", err)
	} else {
		log.Printf("ollama OK (%s)", cfg.Ollama.Endpoint)
	}

	qdrantClient := qdrant.New(cfg.Qdrant.Endpoint)
	if err := qdrantClient.Ping(ctx); err != nil {
		log.Printf("WARN: qdrant unreachable (%v)", err)
	} else {
		log.Printf("qdrant OK (%s)", cfg.Qdrant.Endpoint)
	}

	qwen := ollama.New(cfg.Ollama.Endpoint, cfg.Ollama.TimeoutSeconds)
	r := router.New(router.Config{
		Default:        router.NewOllamaAdapter(qwen, cfg.Ollama.Models["router"]),
		Debugging:      router.NewOllamaAdapter(qwen, cfg.Ollama.Models["router"]),
		Testing:        router.NewOllamaAdapter(qwen, cfg.Ollama.Models["router"]),
		Implementation: router.NewOllamaAdapter(qwen, cfg.Ollama.Models["knowledge"]),
	})

	bearerToken := os.Getenv("LLMO_TRIGGER_TOKEN")

	h := server.Build(server.Config{
		Router: r,
		Health: server.HealthConfig{
			Ollama: ollamaClient,
			Qdrant: qdrantClient,
		},
		BearerToken: bearerToken,
	})

	addr := server.Addr(cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      h,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-quit
	log.Println("shutting down...")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutCtx)
}
