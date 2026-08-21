package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/router"
)

// RouteRequest is the JSON body for POST /v1/route.
type RouteRequest struct {
	Task    string         `json:"task"`
	Context *RouteContext  `json:"context,omitempty"`
}

// RouteContext carries optional metadata about the calling repo/environment.
type RouteContext struct {
	Repo     string `json:"repo,omitempty"`
	Language string `json:"language,omitempty"`
	Branch   string `json:"branch,omitempty"`
}

// RouteResponse is the JSON response for POST /v1/route.
type RouteResponse struct {
	Answer       string `json:"answer"`
	Model        string `json:"model"`
	PromptTokens int    `json:"prompt_tokens"`
	TotalTokens  int    `json:"total_tokens"`
	Error        string `json:"error,omitempty"`
}

type routeHandler struct {
	router *router.Router
}

func (h *routeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRouteErr(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Task) == "" {
		writeRouteErr(w, "task required", http.StatusBadRequest)
		return
	}

	taskType := inferTaskType(req)

	resp, err := h.router.Generate(r.Context(), taskType, req.Task)
	if err != nil {
		writeRouteErr(w, "inference failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(RouteResponse{
		Answer:       resp.Text,
		Model:        resp.Model,
		PromptTokens: resp.PromptTokens,
		TotalTokens:  resp.TotalTokens,
	})
}

// inferTaskType selects a TaskType from the request. Currently rule-based;
// can be replaced with a classifier in future iterations.
func inferTaskType(req RouteRequest) eval.TaskType {
	task := strings.ToLower(req.Task)
	switch {
	case containsAny(task, "debug", "fix", "error", "bug", "fail"):
		return eval.TaskDebugging
	case containsAny(task, "test", "spec", "assert"):
		return eval.TaskTesting
	case containsAny(task, "architect", "design", "structure", "schema"):
		return eval.TaskArchitecture
	default:
		return eval.TaskImplementation
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func writeRouteErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(RouteResponse{Error: msg})
}
