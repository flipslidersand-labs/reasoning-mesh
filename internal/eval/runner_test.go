package eval

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/ollama"
)

// ollamaServer creates a test Ollama server that returns responses in sequence.
// When all responses are consumed, it cycles from the last one.
func ollamaServer(t *testing.T, responses []string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		resp := responses[idx]
		if idx < len(responses)-1 {
			idx++
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response":          resp,
			"done":              true,
			"prompt_eval_count": 10,
			"eval_count":        20,
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// stubRetriever returns a fixed set of KnowledgeItems.
type stubRetriever struct {
	items []KnowledgeItem
	err   error
}

func (s stubRetriever) Retrieve(_ context.Context, _ string, _ TaskType, _ int) ([]KnowledgeItem, error) {
	return s.items, s.err
}

func makeCase(id string, keywords []string) Case {
	c := Case{ID: id, TaskType: TaskDebugging, Prompt: "what is the bug?"}
	c.Expected.RequiredKeywords = keywords
	c.Expected.RootCause = "nil pointer"
	return c
}

func TestRunner_NoRAG_BasicRun(t *testing.T) {
	// main generate returns "nil pointer fix" (contains keyword), judge returns "0.9"
	srv := ollamaServer(t, []string{"nil pointer fix", "0.9"})
	client := ollama.New(srv.URL, 10)

	runner := NewRunnerWithRetrieverMap(client, RetrieverMap{}, []string{"stub-model"}, []Condition{CondNoRAG}).
		WithCooldown(0)

	cases := []Case{makeCase("c1", []string{"nil pointer"})}
	results := runner.Run(context.Background(), cases)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}
	if r.Answer == "" {
		t.Error("expected non-empty answer")
	}
	if r.KeywordRecall != 1.0 {
		t.Errorf("keyword recall = %.2f, want 1.0", r.KeywordRecall)
	}
	if r.Accuracy < 0 {
		t.Errorf("accuracy = %.2f, want >= 0 (judge should have parsed 0.9)", r.Accuracy)
	}
	if r.Condition != CondNoRAG {
		t.Errorf("condition = %q, want %q", r.Condition, CondNoRAG)
	}
	if r.Model != "stub-model" {
		t.Errorf("model = %q, want stub-model", r.Model)
	}
}

func TestRunner_KeywordRecall_Partial(t *testing.T) {
	srv := ollamaServer(t, []string{"only one keyword matches", "-1"})
	client := ollama.New(srv.URL, 10)

	runner := NewRunnerWithRetrieverMap(client, RetrieverMap{}, []string{"m"}, []Condition{CondNoRAG}).
		WithCooldown(0)

	c := makeCase("c1", []string{"one", "missing"})
	results := runner.Run(context.Background(), []Case{c})

	if results[0].KeywordRecall != 0.5 {
		t.Errorf("recall = %.2f, want 0.5", results[0].KeywordRecall)
	}
}

func TestRunner_KeywordRecall_NoKeywords(t *testing.T) {
	srv := ollamaServer(t, []string{"anything", "0.5"})
	client := ollama.New(srv.URL, 10)

	runner := NewRunnerWithRetrieverMap(client, RetrieverMap{}, []string{"m"}, []Condition{CondNoRAG}).
		WithCooldown(0)

	c := Case{ID: "c1", TaskType: TaskDebugging, Prompt: "foo"}
	results := runner.Run(context.Background(), []Case{c})

	if results[0].KeywordRecall != 1.0 {
		t.Errorf("no keywords → recall should be 1.0, got %.2f", results[0].KeywordRecall)
	}
}

func TestRunner_OllamaError_SetsErrorField(t *testing.T) {
	// Server returns empty response → GenerateFull treats it as error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	client := ollama.New(srv.URL, 2) // short timeout

	runner := NewRunnerWithRetrieverMap(client, RetrieverMap{}, []string{"m"}, []Condition{CondNoRAG}).
		WithCooldown(0)

	results := runner.Run(context.Background(), []Case{makeCase("c1", nil)})

	if results[0].Error == "" {
		t.Error("expected error field to be set when Ollama fails")
	}
}

func TestRunner_JudgeAccuracy_ParsesFloat(t *testing.T) {
	// First call = generate answer, second call = judge returns "0.75"
	srv := ollamaServer(t, []string{"good answer here", "0.75"})
	client := ollama.New(srv.URL, 10)

	runner := NewRunnerWithRetrieverMap(client, RetrieverMap{}, []string{"m"}, []Condition{CondNoRAG}).
		WithCooldown(0)

	c := makeCase("c1", nil)
	c.Expected.RootCause = "nil deref"
	results := runner.Run(context.Background(), []Case{c})

	r := results[0]
	if r.Accuracy < 0 {
		t.Errorf("accuracy = %.2f, judge should have parsed 0.75", r.Accuracy)
	}
	if r.Accuracy < 0.74 || r.Accuracy > 0.76 {
		t.Errorf("accuracy = %.2f, want ~0.75", r.Accuracy)
	}
}

func TestRunner_JudgeAccuracy_NoExpected_ReturnsNegOne(t *testing.T) {
	srv := ollamaServer(t, []string{"answer"})
	client := ollama.New(srv.URL, 10)

	runner := NewRunnerWithRetrieverMap(client, RetrieverMap{}, []string{"m"}, []Condition{CondNoRAG}).
		WithCooldown(0)

	// Case with no root_cause and no keywords → judge returns -1
	c := Case{ID: "c1", TaskType: TaskDebugging, Prompt: "foo"}
	results := runner.Run(context.Background(), []Case{c})

	if results[0].Accuracy != -1 {
		t.Errorf("accuracy = %.2f, want -1 when no expected content", results[0].Accuracy)
	}
}

func TestRunner_CosineCond_UsesRetriever(t *testing.T) {
	// Retriever returns 1 item; generate is called with augmented prompt
	called := false
	retriever := stubRetriever{items: []KnowledgeItem{{ID: "k1", Content: "relevant knowledge"}}}
	retrieverFunc := &capturingRetriever{inner: retriever, onRetrieve: func() { called = true }}

	srv := ollamaServer(t, []string{"answer text", "0.8"})
	client := ollama.New(srv.URL, 10)

	rm := RetrieverMap{CondCosine: retrieverFunc}
	runner := NewRunnerWithRetrieverMap(client, rm, []string{"m"}, []Condition{CondCosine}).
		WithCooldown(0)

	runner.Run(context.Background(), []Case{makeCase("c1", nil)})

	if !called {
		t.Error("expected retriever to be called for cosine condition")
	}
}

func TestRunner_CompressedCond_CallsCompressAndGenerate(t *testing.T) {
	// compressed condition: retrieve → compress (1st generate) → main generate (2nd)
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response":          "summary or answer",
			"done":              true,
			"prompt_eval_count": 10,
			"eval_count":        5,
		})
	}))
	defer srv.Close()
	client := ollama.New(srv.URL, 10)

	retriever := stubRetriever{items: []KnowledgeItem{{ID: "k1", Content: "chunk1"}, {ID: "k2", Content: "chunk2"}}}
	rm := RetrieverMap{CondCompressed: retriever}
	runner := NewRunnerWithRetrieverMap(client, rm, []string{"m"}, []Condition{CondCompressed}).
		WithCooldown(0)

	results := runner.Run(context.Background(), []Case{makeCase("c1", nil)})

	if results[0].Error != "" {
		t.Fatalf("unexpected error: %s", results[0].Error)
	}
	// compress call + main generate call + judge call = at least 2 generate calls
	if callCount < 2 {
		t.Errorf("expected at least 2 Ollama calls for compressed condition, got %d", callCount)
	}
}

func TestRunner_WithCooldown_ZeroDoesNotBlock(t *testing.T) {
	srv := ollamaServer(t, []string{"a", "0.5", "b", "0.5"})
	client := ollama.New(srv.URL, 10)

	runner := NewRunnerWithRetrieverMap(client, RetrieverMap{}, []string{"m"}, []Condition{CondNoRAG}).
		WithCooldown(0)

	cases := []Case{makeCase("c1", nil), makeCase("c2", nil)}
	start := time.Now()
	runner.Run(context.Background(), cases)
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Errorf("zero cooldown run took %v, expected < 500ms", elapsed)
	}
}

func TestRunner_MultipleConditions(t *testing.T) {
	// Cycle through enough responses for all conditions
	responses := make([]string, 20)
	for i := range responses {
		responses[i] = "ans"
	}
	srv := ollamaServer(t, responses)
	client := ollama.New(srv.URL, 10)

	conds := []Condition{CondNoRAG, CondCosine}
	runner := NewRunnerWithRetrieverMap(client, RetrieverMap{}, []string{"m"}, conds).
		WithCooldown(0)

	results := runner.Run(context.Background(), []Case{makeCase("c1", nil)})

	if len(results) != 2 {
		t.Errorf("expected 2 results (1 case × 2 conditions), got %d", len(results))
	}
	condSet := map[Condition]bool{}
	for _, r := range results {
		condSet[r.Condition] = true
	}
	for _, c := range conds {
		if !condSet[c] {
			t.Errorf("missing result for condition %q", c)
		}
	}
}

func TestRunner_ContextCancelled_StopsEarly(t *testing.T) {
	srv := ollamaServer(t, []string{"ans"})
	client := ollama.New(srv.URL, 10)

	runner := NewRunnerWithRetrieverMap(client, RetrieverMap{}, []string{"m"}, []Condition{CondNoRAG}).
		WithCooldown(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cases := []Case{makeCase("c1", nil), makeCase("c2", nil), makeCase("c3", nil)}

	// cancel after first result
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	results := runner.Run(ctx, cases)
	// should have fewer than 3 results due to early cancellation
	if len(results) == 3 {
		t.Error("expected early exit on context cancellation, but got all 3 results")
	}
}

// capturingRetriever wraps a Retriever and calls a hook on Retrieve.
type capturingRetriever struct {
	inner      Retriever
	onRetrieve func()
}

func (c *capturingRetriever) Retrieve(ctx context.Context, q string, tt TaskType, k int) ([]KnowledgeItem, error) {
	c.onRetrieve()
	return c.inner.Retrieve(ctx, q, tt, k)
}

// --- compressKnowledge additional tests ---

// TestRunner_CondCompressed_SummaryAppearsInPrompt verifies that the compressed
// summary returned by the first Ollama call is forwarded to the main generate call
// as part of the augmented prompt (not the raw item list).
func TestRunner_CondCompressed_SummaryAppearsInPrompt(t *testing.T) {
	const compressSummary = "UNIQUE_SUMMARY_TOKEN"

	var capturedPrompts []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if p, ok := body["prompt"].(string); ok {
			mu.Lock()
			capturedPrompts = append(capturedPrompts, p)
			mu.Unlock()
		}
		// First call (compress) returns the summary; subsequent calls return a generic answer.
		resp := compressSummary
		if len(capturedPrompts) > 1 {
			resp = "final answer"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": resp, "done": true,
			"prompt_eval_count": 10, "eval_count": 5,
		})
	}))
	defer srv.Close()

	client := ollama.New(srv.URL, 10)
	retriever := stubRetriever{items: []KnowledgeItem{{ID: "k1", Content: "chunk1"}}}
	rm := RetrieverMap{CondCompressed: retriever}
	runner := NewRunnerWithRetrieverMap(client, rm, []string{"m"}, []Condition{CondCompressed}).
		WithCooldown(0)

	results := runner.Run(context.Background(), []Case{makeCase("c1", nil)})
	if results[0].Error != "" {
		t.Fatalf("unexpected error: %s", results[0].Error)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(capturedPrompts) < 2 {
		t.Fatalf("expected at least 2 Ollama calls, got %d", len(capturedPrompts))
	}
	// The second call (main generate) must contain the summary text.
	if !containsString(capturedPrompts[1], compressSummary) {
		t.Errorf("main generate prompt does not contain the compressed summary.\nprompt: %s", capturedPrompts[1])
	}
}

// TestRunner_CondCompressed_FallbackOnCompressError verifies that when the
// compress step errors (server returns 500), the runner falls back to listing
// the raw knowledge items instead of failing the whole case.
func TestRunner_CondCompressed_FallbackOnCompressError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call = compress → simulate error
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Subsequent calls (main generate, judge) succeed.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": "fallback answer", "done": true,
			"prompt_eval_count": 5, "eval_count": 5,
		})
	}))
	defer srv.Close()

	client := ollama.New(srv.URL, 10)
	retriever := stubRetriever{items: []KnowledgeItem{{ID: "k1", Content: "raw chunk"}}}
	rm := RetrieverMap{CondCompressed: retriever}
	runner := NewRunnerWithRetrieverMap(client, rm, []string{"m"}, []Condition{CondCompressed}).
		WithCooldown(0)

	results := runner.Run(context.Background(), []Case{makeCase("c1", nil)})
	// The case should NOT be marked as error — fallback keeps it alive.
	if results[0].Error != "" {
		t.Errorf("expected no error on compress failure (fallback active), got: %s", results[0].Error)
	}
}

func containsString(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) &&
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}()
}
