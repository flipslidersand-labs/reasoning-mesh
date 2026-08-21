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
	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
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

	// --- Ollama ---
	ollamaClient := ollama.New(cfg.Ollama.Endpoint, cfg.Ollama.TimeoutSeconds)
	if err := ollamaClient.Ping(ctx); err != nil {
		return fmt.Errorf("ollama unreachable: %w", err)
	}
	log.Printf("ollama OK (%s)", cfg.Ollama.Endpoint)

	// --- Qdrant ---
	qdrantClient := qdrant.New(cfg.Qdrant.Endpoint)
	if err := qdrantClient.Ping(ctx); err != nil {
		return fmt.Errorf("qdrant unreachable: %w", err)
	}
	log.Printf("qdrant OK (%s)", cfg.Qdrant.Endpoint)

	// --- Embedder ---
	embedder := knowledge.NewEmbedder(cfg.Embedder.Endpoint)

	// --- Scorer ---
	scorer := knowledge.ScorerConfig{
		Alpha:           cfg.Scorer.Alpha,
		Beta:            cfg.Scorer.Beta,
		FreshnessLambda: cfg.Scorer.FreshnessLambda,
		TaskBoost:       cfg.Scorer.TaskBoost,
	}

	// --- Knowledge Retriever (Score condition for production) ---
	knowledgeCol := cfg.Qdrant.Collections["knowledge"]
	retriever := knowledge.NewQdrantRetriever(qdrantClient, embedder, knowledgeCol, scorer, "score")

	// --- Score Updater ---
	updater := knowledge.NewScoreUpdater(qdrantClient, knowledgeCol, 256)
	updater.Start()
	defer updater.Stop()

	// --- Knowledge Extractor ---
	ollamaStructurizer := knowledge.NewStructurizer(ollamaClient, cfg.Ollama.Models["router"])
	extractor := knowledge.NewExtractor(ollamaStructurizer, embedder, qdrantClient, knowledgeCol)

	// --- Router ---
	routerModel := cfg.Ollama.Models["router"]
	knowledgeModel := cfg.Ollama.Models["knowledge"]

	qwen := router.NewOllamaAdapter(ollamaClient, routerModel)
	ornith := router.NewOllamaAdapter(ollamaClient, knowledgeModel)

	var claudeAdapter router.ModelAdapter
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		claudeAdapter = router.NewClaudeAdapter(key, "")
		log.Printf("claude adapter enabled")
	} else {
		claudeAdapter = ornith // fallback to knowledge model
		log.Printf("ANTHROPIC_API_KEY not set — architecture tasks → ornith")
	}

	r := router.New(router.Config{
		Debugging:      qwen,
		Testing:        qwen,
		Implementation: ornith,
		Architecture:   claudeAdapter,
		Default:        qwen,
	})

	// --- Bearer Token ---
	bearerToken := os.Getenv("LLMO_TRIGGER_TOKEN")

	// --- HTTP Server ---
	handler := server.Build(server.Config{
		Router:       r,
		Extractor:    extractor,
		ScoreUpdater: updater,
		Retriever:    retriever,
		BearerToken:  bearerToken,
	})

	addr := server.Addr(cfg.Server.Host, cfg.Server.Port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 180 * time.Second,
	}

	log.Printf("reasoning-mesh listening on %s", addr)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	<-quit
	log.Println("shutting down...")

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutCtx)
}
