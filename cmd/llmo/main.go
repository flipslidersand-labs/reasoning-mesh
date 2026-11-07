package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/bench"
	"github.com/flipslidersand/reasoning-mesh/internal/config"
	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/ollama"
	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
	"github.com/flipslidersand/reasoning-mesh/internal/validate"
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
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	switch os.Args[1] {
	case "ask":
		if err := runAsk(cfg, os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "ingest":
		if err := runIngest(cfg, os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "eval":
		runEval(cfg, os.Args[2:])
	case "bench":
		runBench(os.Args[2:])
	case "ingest-local":
		runIngestLocal(cfg, os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: llmo <command> [flags]")
	fmt.Println("Commands:")
	fmt.Println("  ask          Send a task to the llmo server and print the response")
	fmt.Println("  ingest       Ingest a git commit or file into the knowledge base")
	fmt.Println("  eval         Run eval harness against test cases")
	fmt.Println("  bench        Show longitudinal benchmark across result files")
	fmt.Println("  ingest-local Directly ingest a knowledge entry into Qdrant")
}

func runEval(cfg *config.Config, args []string) {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	casesDir := fs.String("cases", "cases", "directory containing YAML test cases")
	outDir := fs.String("out", "results", "directory to write JSON results")
	modelsFlag := fs.String("models", "", "comma-separated model names (default: router+knowledge from config)")
	judgeFlag := fs.String("judge-model", "", "model that scores answers (default: first of -models; a single model then grades itself)")
	condFlag := fs.String("conditions", "all", "conditions to run: all | no-rag | cosine | score | compressed")
	allowNoop := fs.Bool("allow-noop-rag", false, "continue with NoopRetriever if Qdrant is unreachable (results are marked rag_degraded)")
	cooldownSec := fs.Int("cooldown", 5, "seconds to wait between requests (increase to prevent GPU thermal throttling)")
	_ = fs.Parse(args)

	cases, err := eval.LoadCases(*casesDir)
	if err != nil {
		log.Fatalf("load cases: %v", err)
	}
	if len(cases) == 0 {
		log.Fatalf("no cases found in %s", *casesDir)
	}
	fmt.Printf("Loaded %d cases from %s\n", len(cases), *casesDir)

	models, err := resolveModels(cfg, *modelsFlag)
	if err != nil {
		log.Fatalf("%v", err)
	}
	conditions := resolveConditions(*condFlag)

	ollamaClient := ollama.New(cfg.Ollama.Endpoint, cfg.Ollama.TimeoutSeconds).WithNumPredict(cfg.Ollama.NumPredict)
	ctx := context.Background()

	if err := ollamaClient.Ping(ctx); err != nil {
		log.Fatalf("ollama unreachable: %v", err)
	}

	rm := eval.RetrieverMap{}
	ragDegraded := false
	if needsRAG(conditions) {
		rm, ragDegraded, err = buildRetrieverMap(ctx, cfg, *allowNoop)
		if err != nil {
			log.Fatalf("%v", err)
		}
	}
	runner := eval.NewRunnerWithRetrieverMap(ollamaClient, rm, models, conditions).
		WithCooldown(time.Duration(*cooldownSec) * time.Second)
	if *judgeFlag != "" {
		runner.WithJudgeModel(*judgeFlag)
	}
	if runner.JudgeModel() == "" {
		log.Fatalf("no judge model: pass -models or -judge-model")
	}
	if runner.SelfJudging() {
		log.Printf("WARN: %q judges its own answers; accuracy is not comparable with baselines (pass -judge-model)", runner.JudgeModel())
	}
	results := runner.Run(ctx, cases)

	fmt.Println()
	eval.PrintTable(results)
	eval.PrintSummary(eval.ComputeSummaries(results))

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		log.Fatalf("mkdir %s: %v", *outDir, err)
	}
	outPath := filepath.Join(*outDir, time.Now().Format("20060102-150405")+".json")
	if err := eval.SaveRunJSON(results, outPath, ragDegraded); err != nil {
		log.Fatalf("save results: %v", err)
	}
	fmt.Printf("\nResults saved to %s\n", outPath)
}

func needsRAG(conds []eval.Condition) bool {
	for _, c := range conds {
		if c != eval.CondNoRAG {
			return true
		}
	}
	return false
}

// buildRetrieverMap wires Qdrant + e5 embedder into per-condition retrievers.
// If Qdrant is unreachable it returns an error, unless allowNoop is set, in
// which case all RAG conditions use NoopRetriever and degraded is true.
func buildRetrieverMap(ctx context.Context, cfg *config.Config, allowNoop bool) (rm eval.RetrieverMap, degraded bool, err error) {
	noop := eval.NoopRetriever{}
	rm = eval.RetrieverMap{
		eval.CondCosine:     noop,
		eval.CondScore:      noop,
		eval.CondCompressed: noop,
	}

	qc := qdrant.New(cfg.Qdrant.Endpoint).WithAPIKey(cfg.Qdrant.APIKey)
	if perr := qc.Ping(ctx); perr != nil {
		if !allowNoop {
			return nil, false, fmt.Errorf("Qdrant unreachable (%v); RAG results would equal no-rag (pass -allow-noop-rag to continue)", perr)
		}
		log.Printf("WARN: Qdrant unreachable (%v) — RAG conditions use NoopRetriever; result marked rag_degraded", perr)
		return rm, true, nil
	}

	emb := knowledge.NewEmbedderWithAuth(cfg.Embedder.Endpoint, cfg.Embedder.APIKey, cfg.Embedder.Collection)
	scorer := cfg.Scorer
	col := cfg.Qdrant.Collections["knowledge"]

	rm[eval.CondCosine] = knowledge.NewQdrantRetriever(qc, emb, col, scorer, eval.CondCosine)
	rm[eval.CondScore] = knowledge.NewQdrantRetriever(qc, emb, col, scorer, eval.CondScore)
	rm[eval.CondCompressed] = knowledge.NewQdrantRetriever(qc, emb, col, scorer, eval.CondCosine) // same as cosine; compression done in runner
	log.Printf("Qdrant OK — RAG conditions wired (%s)", cfg.Qdrant.Endpoint)
	return rm, false, nil
}

// resolveModels returns the non-empty model names from the flag, or from
// ollama.models (router, knowledge) when the flag is empty. It returns an
// error if no model name remains.
func resolveModels(cfg *config.Config, flag string) ([]string, error) {
	candidates := splitComma(flag)
	hint := "pass at least one model name to -models"
	if flag == "" {
		candidates = []string{cfg.Ollama.Models["router"], cfg.Ollama.Models["knowledge"]}
		hint = "set ollama.models.router/knowledge in config"
	}
	var models []string
	for _, m := range candidates {
		if m = strings.TrimSpace(m); m != "" {
			models = append(models, m)
		}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("no model names resolved: %s", hint)
	}
	return models, nil
}

func resolveConditions(flag string) []eval.Condition {
	if flag == "all" || flag == "" {
		return eval.AllConditions
	}
	valid := make(map[eval.Condition]bool, len(eval.AllConditions))
	for _, c := range eval.AllConditions {
		valid[c] = true
	}
	var conds []eval.Condition
	for _, s := range splitComma(flag) {
		c := eval.Condition(s)
		if !valid[c] {
			log.Fatalf("unknown condition: %s (valid: no-rag,cosine,score,compressed)", s)
		}
		conds = append(conds, c)
	}
	return conds
}

func runIngestLocal(cfg *config.Config, args []string) {
	fs := flag.NewFlagSet("ingest-local", flag.ExitOnError)
	contentFlag := fs.String("content", "", "knowledge text to ingest")
	fileFlag := fs.String("file", "", "read content from file (overrides -content)")
	taskType := fs.String("task-type", "implementation", "task type: debugging|implementation|architecture|testing")
	language := fs.String("language", "", "programming language (e.g. go, rust)")
	framework := fs.String("framework", "", "framework name (e.g. gin, actix)")
	tagsFlag := fs.String("tags", "", "comma-separated tags")
	_ = fs.Parse(args)

	if err := validate.TaskType(*taskType); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		fs.Usage()
		os.Exit(1)
	}

	content := *contentFlag
	if *fileFlag != "" {
		data, err := os.ReadFile(*fileFlag)
		if err != nil {
			log.Fatalf("read file: %v", err)
		}
		content = string(data)
	}
	if content == "" {
		fmt.Fprintln(os.Stderr, "error: -content or -file is required")
		fs.Usage()
		os.Exit(1)
	}

	ctx := context.Background()

	qc := qdrant.New(cfg.Qdrant.Endpoint).WithAPIKey(cfg.Qdrant.APIKey)
	if err := qc.Ping(ctx); err != nil {
		log.Fatalf("qdrant unreachable: %v", err)
	}

	emb := knowledge.NewEmbedderWithAuth(cfg.Embedder.Endpoint, cfg.Embedder.APIKey, cfg.Embedder.Collection)
	col := cfg.Qdrant.Collections["knowledge"]
	store := knowledge.NewIngestStore(emb, qc, col)

	entry := knowledge.IngestEntry{
		Content:   content,
		TaskType:  eval.TaskType(*taskType),
		Language:  *language,
		Framework: *framework,
		Tags:      splitComma(*tagsFlag),
	}

	id, err := store.Ingest(ctx, entry)
	if err != nil {
		log.Fatalf("ingest: %v", err)
	}
	fmt.Printf("ingested: %s\n", id)
}

func runBench(args []string) {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	resultsDir := fs.String("results", "results", "directory containing eval result JSON files")
	_ = fs.Parse(args)

	runs, err := bench.Load(*resultsDir)
	if err != nil {
		log.Fatalf("bench load: %v", err)
	}
	if len(runs) == 0 {
		fmt.Printf("No result files found in %s\n", *resultsDir)
		return
	}

	deltas := bench.ComputeDeltas(runs)
	bench.PrintDeltas(runs, deltas)
}

func splitComma(s string) []string {
	out := strings.Split(s, ",")
	for i := range out {
		out[i] = strings.TrimSpace(out[i])
	}
	return out
}
