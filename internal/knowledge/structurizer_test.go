package knowledge_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/ollama"
)

func ollamaStub(t *testing.T, response string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":    "stub",
			"response": response,
			"done":     true,
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func ollamaError(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStructurize_ValidJSON(t *testing.T) {
	llmResp := `{"task_type":"debugging","language":"go","framework":"net/http","tags":["error-handling","middleware"]}`
	srv := ollamaStub(t, llmResp)
	s := knowledge.NewStructurizer(ollama.New(srv.URL, 10), "stub")

	chunk := knowledge.Chunk{Content: "if err != nil { return err }", HintTaskType: eval.TaskImplementation}
	got, err := s.Structurize(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TaskType != eval.TaskDebugging {
		t.Errorf("task_type = %q, want debugging", got.TaskType)
	}
	if got.Language != "go" {
		t.Errorf("language = %q, want go", got.Language)
	}
	if got.Framework != "net/http" {
		t.Errorf("framework = %q, want net/http", got.Framework)
	}
	if len(got.Tags) != 2 {
		t.Errorf("tags = %v, want 2 items", got.Tags)
	}
}

func TestStructurize_UnknownTaskType_FallsBackToHint(t *testing.T) {
	llmResp := `{"task_type":"refactoring","language":"rust","framework":"","tags":[]}`
	srv := ollamaStub(t, llmResp)
	s := knowledge.NewStructurizer(ollama.New(srv.URL, 10), "stub")

	chunk := knowledge.Chunk{Content: "fn main() {}", HintTaskType: eval.TaskArchitecture}
	got, _ := s.Structurize(context.Background(), chunk)
	if got.TaskType != eval.TaskArchitecture {
		t.Errorf("unknown task_type should fall back to hint, got %q", got.TaskType)
	}
}

func TestStructurize_NoisyLLMOutput(t *testing.T) {
	// LLM wraps JSON in prose — extractor should strip and parse the JSON block.
	llmResp := "Here is the metadata:\n" +
		`{"task_type":"testing","language":"typescript","framework":"jest","tags":["unit"]}` +
		"\nHope that helps!"
	srv := ollamaStub(t, llmResp)
	s := knowledge.NewStructurizer(ollama.New(srv.URL, 10), "stub")

	chunk := knowledge.Chunk{Content: "describe('auth', () => {})", HintTaskType: eval.TaskImplementation}
	got, _ := s.Structurize(context.Background(), chunk)
	if got.TaskType != eval.TaskTesting {
		t.Errorf("noisy output: task_type = %q, want testing", got.TaskType)
	}
	if got.Language != "typescript" {
		t.Errorf("noisy output: language = %q, want typescript", got.Language)
	}
}

func TestStructurize_InvalidJSON_FallsBackToHint(t *testing.T) {
	srv := ollamaStub(t, "not json at all")
	s := knowledge.NewStructurizer(ollama.New(srv.URL, 10), "stub")

	chunk := knowledge.Chunk{Content: "some code", HintTaskType: eval.TaskDebugging}
	got, _ := s.Structurize(context.Background(), chunk)
	if got.TaskType != eval.TaskDebugging {
		t.Errorf("invalid JSON should fall back to hint, got %q", got.TaskType)
	}
	if got.Content != chunk.Content {
		t.Errorf("content should be preserved, got %q", got.Content)
	}
}

func TestStructurize_OllamaError_FallsBackToHint(t *testing.T) {
	srv := ollamaError(t)
	s := knowledge.NewStructurizer(ollama.New(srv.URL, 10), "stub")

	chunk := knowledge.Chunk{Content: "panic here", HintTaskType: eval.TaskDebugging}
	got, _ := s.Structurize(context.Background(), chunk)
	if got.TaskType != eval.TaskDebugging {
		t.Errorf("ollama error should fall back to hint, got %q", got.TaskType)
	}
}

func TestStructurize_AllTaskTypes(t *testing.T) {
	cases := []struct {
		llmType  string
		wantType eval.TaskType
	}{
		{"debugging", eval.TaskDebugging},
		{"implementation", eval.TaskImplementation},
		{"architecture", eval.TaskArchitecture},
		{"testing", eval.TaskTesting},
	}
	for _, tc := range cases {
		llmResp := `{"task_type":"` + tc.llmType + `","language":"go","framework":"","tags":[]}`
		srv := ollamaStub(t, llmResp)
		s := knowledge.NewStructurizer(ollama.New(srv.URL, 10), "stub")
		chunk := knowledge.Chunk{Content: "code", HintTaskType: eval.TaskImplementation}
		got, _ := s.Structurize(context.Background(), chunk)
		if got.TaskType != tc.wantType {
			t.Errorf("%s: task_type = %q, want %q", tc.llmType, got.TaskType, tc.wantType)
		}
	}
}
