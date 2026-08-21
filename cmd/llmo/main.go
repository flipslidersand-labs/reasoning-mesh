package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/config"
	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/ollama"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cfgPath := "config/config.yaml"
	if v := os.Getenv("LLMO_CONFIG"); v != "" {
		cfgPath = v
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	switch os.Args[1] {
	case "eval":
		runEval(cfg, os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: llmo <command> [flags]")
	fmt.Println("Commands:")
	fmt.Println("  eval    Run eval harness against test cases")
}

func runEval(cfg *config.Config, args []string) {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	casesDir := fs.String("cases", "cases", "directory containing YAML test cases")
	outDir := fs.String("out", "results", "directory to write JSON results")
	modelsFlag := fs.String("models", "", "comma-separated model names (default: router+knowledge from config)")
	condFlag := fs.String("conditions", "all", "conditions to run: all | no-rag | cosine | score | compressed")
	_ = fs.Parse(args)

	cases, err := eval.LoadCases(*casesDir)
	if err != nil {
		log.Fatalf("load cases: %v", err)
	}
	if len(cases) == 0 {
		log.Fatalf("no cases found in %s", *casesDir)
	}
	fmt.Printf("Loaded %d cases from %s\n", len(cases), *casesDir)

	models := resolveModels(cfg, *modelsFlag)
	conditions := resolveConditions(*condFlag)

	ollamaClient := ollama.New(cfg.Ollama.Endpoint, cfg.Ollama.TimeoutSeconds)
	ctx := context.Background()

	if err := ollamaClient.Ping(ctx); err != nil {
		log.Fatalf("ollama unreachable: %v", err)
	}

	runner := eval.NewRunnerWithConditions(ollamaClient, eval.NoopRetriever{}, models, conditions)
	results := runner.Run(ctx, cases)

	fmt.Println()
	eval.PrintTable(results)

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		log.Fatalf("mkdir %s: %v", *outDir, err)
	}
	outPath := filepath.Join(*outDir, time.Now().Format("20060102-150405")+".json")
	if err := eval.SaveJSON(results, outPath); err != nil {
		log.Fatalf("save results: %v", err)
	}
	fmt.Printf("\nResults saved to %s\n", outPath)
}

func resolveModels(cfg *config.Config, flag string) []string {
	if flag != "" {
		var models []string
		for _, m := range splitComma(flag) {
			if m != "" {
				models = append(models, m)
			}
		}
		return models
	}
	return []string{cfg.Ollama.Models["router"], cfg.Ollama.Models["knowledge"]}
}

func resolveConditions(flag string) []eval.Condition {
	if flag == "all" || flag == "" {
		return eval.AllConditions
	}
	var conds []eval.Condition
	for _, s := range splitComma(flag) {
		conds = append(conds, eval.Condition(s))
	}
	return conds
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return out
}
