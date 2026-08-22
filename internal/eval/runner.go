package eval

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/ollama"
)

const topK = 3

// RetrieverMap maps each condition to its Retriever.
// Conditions not present in the map fall back to NoopRetriever.
type RetrieverMap map[Condition]Retriever

// Runner executes eval cases across models and conditions.
type Runner struct {
	ollama         *ollama.Client
	retrievers     RetrieverMap
	models         []string
	conditions     []Condition
	feedbackSender FeedbackSender // optional; nil = no auto-feedback
}

// NewRunner creates a Runner that uses the same retriever for all RAG conditions.
func NewRunner(ollamaClient *ollama.Client, retriever Retriever, models []string) *Runner {
	return NewRunnerWithConditions(ollamaClient, retriever, models, AllConditions)
}

// NewRunnerWithConditions creates a Runner with a single retriever for all RAG conditions.
func NewRunnerWithConditions(ollamaClient *ollama.Client, retriever Retriever, models []string, conditions []Condition) *Runner {
	rm := RetrieverMap{
		CondCosine:     retriever,
		CondScore:      retriever,
		CondCompressed: retriever,
	}
	return NewRunnerWithRetrieverMap(ollamaClient, rm, models, conditions)
}

// NewRunnerWithRetrieverMap creates a Runner with per-condition retrievers.
// Use this when cosine and score conditions need different QdrantRetriever instances.
func NewRunnerWithRetrieverMap(ollamaClient *ollama.Client, rm RetrieverMap, models []string, conditions []Condition) *Runner {
	return &Runner{
		ollama:     ollamaClient,
		retrievers: rm,
		models:     models,
		conditions: conditions,
	}
}

// WithFeedbackSender attaches an optional FeedbackSender.
// When set, the runner posts feedback to /v1/feedback after every scored eval run.
func (r *Runner) WithFeedbackSender(fs FeedbackSender) *Runner {
	r.feedbackSender = fs
	return r
}

func (r *Runner) retrieverFor(cond Condition) Retriever {
	if ret, ok := r.retrievers[cond]; ok {
		return ret
	}
	return NoopRetriever{}
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

				// Brief cooldown between requests to prevent GPU thermal throttling
				// on shared Ollama instances running large models (e.g. ornith:9b).
				if done < total {
					select {
					case <-ctx.Done():
						return results
					case <-time.After(5 * time.Second):
					}
				}
			}
		}
	}
	return results
}

// promptResult bundles the built prompt with the knowledge IDs used to build it.
type promptResult struct {
	Prompt       string
	KnowledgeIDs []string
}

func (r *Runner) runOne(ctx context.Context, c Case, model string, cond Condition) Result {
	res := Result{
		CaseID:    c.ID,
		TaskType:  c.TaskType,
		Model:     model,
		Condition: cond,
	}

	pr, err := r.buildPrompt(ctx, c, cond)
	if err != nil {
		res.Error = fmt.Sprintf("build prompt: %v", err)
		return res
	}

	start := time.Now()
	genResp, err := r.ollama.GenerateFull(ctx, model, pr.Prompt)
	res.LatencyMS = time.Since(start).Milliseconds()

	if err != nil {
		res.Error = err.Error()
		return res
	}

	res.Answer = genResp.Response
	if genResp.PromptEvalCount > 0 {
		res.PromptTokens = genResp.PromptEvalCount
	} else {
		res.PromptTokens = len(strings.Fields(pr.Prompt))
	}
	if genResp.EvalCount > 0 {
		res.CompletionTokens = genResp.EvalCount
	} else {
		res.CompletionTokens = len(strings.Fields(genResp.Response))
	}
	res.KeywordRecall = res.keywordRecall(genResp.Response, c.Expected.RequiredKeywords)
	res.Accuracy = r.judgeAccuracy(ctx, c, genResp.Response)

	// Auto-feedback: non-blocking, non-fatal
	if r.feedbackSender != nil && len(pr.KnowledgeIDs) > 0 && res.Accuracy >= 0 {
		success := res.Accuracy >= 0.7
		if err := r.feedbackSender.Send(ctx, pr.KnowledgeIDs, success, "eval"); err != nil {
			log.Printf("feedback: %v", err)
		}
	}

	return res
}

func (r *Runner) buildPrompt(ctx context.Context, c Case, cond Condition) (promptResult, error) {
	var sb strings.Builder
	var knowledgeIDs []string

	if cond != CondNoRAG {
		items, err := r.retrieverFor(cond).Retrieve(ctx, c.Prompt, c.TaskType, topK)
		if err != nil {
			return promptResult{}, fmt.Errorf("retrieve: %w", err)
		}
		if len(items) > 0 {
			for _, item := range items {
				knowledgeIDs = append(knowledgeIDs, item.ID)
			}
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

	sb.WriteString(c.Prompt)

	if c.Context.Snippet != "" {
		sb.WriteString("\n\n```" + c.Context.Language + "\n")
		sb.WriteString(c.Context.Snippet)
		sb.WriteString("\n```")
	}

	return promptResult{Prompt: sb.String(), KnowledgeIDs: knowledgeIDs}, nil
}

// judgeAccuracy uses qwen2.5:7b to score the answer against the expected output (0.0-1.0).
// Returns -1 on error (judge unavailable), so callers can distinguish "not scored" from 0.
func (r *Runner) judgeAccuracy(ctx context.Context, c Case, answer string) float64 {
	expected := c.Expected.RootCause
	if expected == "" && len(c.Expected.RequiredKeywords) > 0 {
		expected = "must include: " + strings.Join(c.Expected.RequiredKeywords, ", ")
	}
	if expected == "" {
		return -1
	}

	judgePrompt := fmt.Sprintf(`以下の回答を評価してください。
期待される内容: %s

実際の回答:
%s

この回答は期待される内容を正しく含んでいますか？
0.0（全く不正解）から1.0（完全正解）の数値のみ返してください。数値以外は出力しないでください。`, expected, answer)

	// Use the same model as the eval target to avoid model swapping on a shared Ollama instance.
	judgeModel := r.models[0]
	raw, err := r.ollama.Generate(ctx, judgeModel, judgePrompt)
	if err != nil {
		return -1
	}
	raw = strings.TrimSpace(raw)
	var score float64
	if _, err := fmt.Sscanf(raw, "%f", &score); err != nil {
		return -1
	}
	if score < 0 {
		score = 0
	} else if score > 1 {
		score = 1
	}
	return score
}

// compressKnowledge summarises retrieved items into a single paragraph using the eval model.
func (r *Runner) compressKnowledge(ctx context.Context, items []KnowledgeItem) (string, error) {
	var raw strings.Builder
	for i, item := range items {
		raw.WriteString(fmt.Sprintf("%d. %s\n", i+1, item.Content))
	}
	compressPrompt := fmt.Sprintf(
		"以下のナレッジを1段落（200字以内）に要約してください。重要な技術キーワードを保持すること。\n\n%s",
		raw.String(),
	)
	return r.ollama.Generate(ctx, r.models[0], compressPrompt)
}
