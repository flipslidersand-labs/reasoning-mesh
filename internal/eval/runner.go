package eval

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/ollama"
)

const topK = 3

// Runner executes eval cases across models and conditions.
type Runner struct {
	ollama     *ollama.Client
	retriever  Retriever // NoopRetriever until Phase 2
	models     []string
	conditions []Condition
}

func NewRunner(ollamaClient *ollama.Client, retriever Retriever, models []string) *Runner {
	return NewRunnerWithConditions(ollamaClient, retriever, models, AllConditions)
}

func NewRunnerWithConditions(ollamaClient *ollama.Client, retriever Retriever, models []string, conditions []Condition) *Runner {
	return &Runner{
		ollama:     ollamaClient,
		retriever:  retriever,
		models:     models,
		conditions: conditions,
	}
}

// Run executes all cases × all models × all conditions and returns results.
func (r *Runner) Run(ctx context.Context, cases []Case) []Result {
	var results []Result
	total := len(cases) * len(r.models) * len(r.conditions)
	done := 0

	for _, c := range cases {
		for _, model := range r.models {
			for _, cond := range r.conditions {
				done++
				fmt.Printf("[%d/%d] %s × %s × %s\n", done, total, c.ID, model, cond)

				res := r.runOne(ctx, c, model, cond)
				results = append(results, res)
			}
		}
	}
	return results
}

func (r *Runner) runOne(ctx context.Context, c Case, model string, cond Condition) Result {
	res := Result{
		CaseID:    c.ID,
		TaskType:  c.TaskType,
		Model:     model,
		Condition: cond,
	}

	prompt, err := r.buildPrompt(ctx, c, cond)
	if err != nil {
		res.Error = fmt.Sprintf("build prompt: %v", err)
		return res
	}

	start := time.Now()
	answer, err := r.ollama.Generate(ctx, model, prompt)
	res.LatencyMS = time.Since(start).Milliseconds()

	if err != nil {
		res.Error = err.Error()
		return res
	}

	res.Answer = answer
	// Approximate token counts (Ollama doesn't always return token counts in generate mode)
	res.PromptTokens = len(strings.Fields(prompt))
	res.CompletionTokens = len(strings.Fields(answer))
	res.KeywordRecall = res.keywordRecall(answer, c.Expected.RequiredKeywords)

	return res
}

func (r *Runner) buildPrompt(ctx context.Context, c Case, cond Condition) (string, error) {
	var sb strings.Builder

	// Conditions B/C/D: prepend retrieved knowledge
	if cond != CondNoRAG {
		items, err := r.retriever.Retrieve(ctx, c.Prompt, c.TaskType, topK)
		if err != nil {
			return "", fmt.Errorf("retrieve: %w", err)
		}
		if len(items) > 0 {
			switch cond {
			case CondCosine, CondScore:
				sb.WriteString("## 関連ナレッジ\n")
				for i, item := range items {
					sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, item.Content))
				}
				sb.WriteString("\n")
			case CondCompressed:
				summary, err := r.compressKnowledge(ctx, items)
				if err != nil {
					// fallback: use raw knowledge
					sb.WriteString("## 関連ナレッジ（要約）\n")
					for _, item := range items {
						sb.WriteString(item.Content + "\n")
					}
				} else {
					sb.WriteString("## 関連ナレッジ（要約）\n")
					sb.WriteString(summary + "\n\n")
				}
			}
		}
	}

	// Main prompt
	sb.WriteString(c.Prompt)

	// Append context snippet if present
	if c.Context.Snippet != "" {
		sb.WriteString("\n\n```" + c.Context.Language + "\n")
		sb.WriteString(c.Context.Snippet)
		sb.WriteString("\n```")
	}

	return sb.String(), nil
}

// compressKnowledge uses qwen2.5:7b to summarise retrieved items into a single paragraph.
func (r *Runner) compressKnowledge(ctx context.Context, items []KnowledgeItem) (string, error) {
	var raw strings.Builder
	for i, item := range items {
		raw.WriteString(fmt.Sprintf("%d. %s\n", i+1, item.Content))
	}
	compressPrompt := fmt.Sprintf(
		"以下のナレッジを1段落（200字以内）に要約してください。重要な技術キーワードを保持すること。\n\n%s",
		raw.String(),
	)
	// Always use the router model (qwen2.5:7b) for compression to keep costs low
	return r.ollama.Generate(ctx, "qwen2.5:7b", compressPrompt)
}
