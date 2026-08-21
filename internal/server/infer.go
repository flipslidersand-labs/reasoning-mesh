package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/router"
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

	var req InferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
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

	// RAG: retrieve relevant knowledge items
	var knowledgeIDs []string
	augmentedPrompt := req.Prompt
	if h.retriever != nil {
		items, err := h.retriever.Retrieve(r.Context(), req.Prompt, taskType, topK)
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
	resp, err := h.router.Generate(r.Context(), taskType, augmentedPrompt)
	if err != nil {
		writeErr(w, "inference failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

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
