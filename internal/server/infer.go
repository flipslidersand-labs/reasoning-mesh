package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/router"
	"github.com/flipslidersand/reasoning-mesh/internal/telemetry"
)

// InferRetriever is the interface the infer handler needs for RAG lookup.
type InferRetriever interface {
	Retrieve(ctx context.Context, query string, taskType eval.TaskType, topK int) ([]eval.KnowledgeItem, error)
}

// InferRequest is the JSON body for POST /v1/infer.
type InferRequest struct {
	Prompt   string `json:"prompt"`
	TaskType string `json:"task_type"` // debugging|implementation|architecture|testing
	TopK     int    `json:"top_k"`     // default 3
}

// InferResponse is the JSON response for POST /v1/infer.
type InferResponse struct {
	Answer       string   `json:"answer"`
	Model        string   `json:"model"`
	PromptTokens int      `json:"prompt_tokens"`
	TotalTokens  int      `json:"total_tokens"`
	KnowledgeIDs []string `json:"knowledge_ids"` // used for feedback linkage (#28)
	Error        string   `json:"error,omitempty"`
}

type inferHandler struct {
	router    *router.Router
	retriever InferRetriever
	updater   *knowledge.ScoreUpdater
}

func (h *inferHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, span := telemetry.Tracer("server/infer").Start(r.Context(), "infer")
	defer span.End()

	var req InferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.SetStatus(codes.Error, err.Error())
		writeErr(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		span.SetStatus(codes.Error, "prompt required")
		writeErr(w, "prompt required", http.StatusBadRequest)
		return
	}

	taskType := eval.TaskType(req.TaskType)
	if taskType == "" {
		taskType = eval.TaskImplementation
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 3
	}
	span.SetAttributes(
		attribute.String("task_type", string(taskType)),
		attribute.Int("top_k", topK),
	)

	// RAG: retrieve relevant knowledge items
	var knowledgeIDs []string
	augmentedPrompt := req.Prompt
	if h.retriever != nil {
		ragCtx, ragSpan := telemetry.Tracer("server/infer").Start(ctx, "rag_retrieve")
		items, err := h.retriever.Retrieve(ragCtx, req.Prompt, taskType, topK)
		ragSpan.SetAttributes(attribute.Int("retrieved_count", len(items)))
		ragSpan.End()
		if err == nil && len(items) > 0 {
			var sb strings.Builder
			sb.WriteString("## 関連ナレッジ\n")
			for i, item := range items {
				sb.WriteString(item.Content)
				if i < len(items)-1 {
					sb.WriteString("\n")
				}
				knowledgeIDs = append(knowledgeIDs, item.ID)
			}
			sb.WriteString("\n\n")
			sb.WriteString(req.Prompt)
			augmentedPrompt = sb.String()
		}
	}

	// Route to appropriate model adapter
	genCtx, genSpan := telemetry.Tracer("server/infer").Start(ctx, "generate")
	resp, err := h.router.Generate(genCtx, taskType, augmentedPrompt)
	if err != nil {
		genSpan.SetStatus(codes.Error, err.Error())
		genSpan.End()
		span.SetStatus(codes.Error, err.Error())
		writeErr(w, "inference failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	genSpan.SetAttributes(
		attribute.String("model", resp.Model),
		attribute.Int("prompt_tokens", resp.PromptTokens),
		attribute.Int("total_tokens", resp.TotalTokens),
	)
	genSpan.End()

	span.SetAttributes(
		attribute.String("model", resp.Model),
		attribute.Int("knowledge_id_count", len(knowledgeIDs)),
	)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(InferResponse{
		Answer:       resp.Text,
		Model:        resp.Model,
		PromptTokens: resp.PromptTokens,
		TotalTokens:  resp.TotalTokens,
		KnowledgeIDs: knowledgeIDs,
	})
}

func writeErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(InferResponse{Error: msg})
}
