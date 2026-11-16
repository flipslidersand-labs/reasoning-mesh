package router_test

import (
	"context"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/router"
)

// stubAdapter is a test double that records which adapter was called.
type stubAdapter struct{ name string }

func (s *stubAdapter) Name() string { return s.name }
func (s *stubAdapter) Generate(_ context.Context, _ string) (router.Response, error) {
	return router.Response{Text: "ok", Model: s.name}, nil
}

func mustNew(t *testing.T, cfg router.Config) *router.Router {
	t.Helper()
	r, err := router.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func TestRouter_New_NilDefault(t *testing.T) {
	a := &stubAdapter{name: "a"}
	cases := []struct {
		name string
		cfg  router.Config
	}{
		{"empty config", router.Config{}},
		{"per-task set but no default", router.Config{Debugging: a, Implementation: a, Architecture: a, Testing: a}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := router.New(tc.cfg)
			if err == nil {
				t.Fatal("New with nil Default should return error")
			}
			if r != nil {
				t.Errorf("router should be nil on error, got %v", r)
			}
		})
	}
}

func TestRouter_NilTaskAdapterFallsBackToDefault(t *testing.T) {
	def := &stubAdapter{name: "default"}
	own := &stubAdapter{name: "own"}
	cases := []struct {
		name string
		cfg  router.Config
		task eval.TaskType
		want string
	}{
		{"debugging nil", router.Config{Default: def, Implementation: own}, eval.TaskDebugging, "default"},
		{"testing nil", router.Config{Default: def, Implementation: own}, eval.TaskTesting, "default"},
		{"architecture nil", router.Config{Default: def, Implementation: own}, eval.TaskArchitecture, "default"},
		{"implementation set", router.Config{Default: def, Implementation: own}, eval.TaskImplementation, "own"},
		{"unknown task", router.Config{Default: def, Implementation: own}, "unknown", "default"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := mustNew(t, tc.cfg)
			if got := r.Select(tc.task).Name(); got != tc.want {
				t.Errorf("Select(%s) = %q, want %q", tc.task, got, tc.want)
			}
			if _, err := r.Generate(context.Background(), tc.task, "p"); err != nil {
				t.Errorf("Generate: %v", err)
			}
		})
	}
}

func TestRouter_SelectByTaskType(t *testing.T) {
	qwen := &stubAdapter{name: "ollama/qwen2.5:7b"}
	ornith := &stubAdapter{name: "ollama/ornith:9b"}
	claude := &stubAdapter{name: "claude/claude-sonnet-4-6"}

	r := mustNew(t, router.Config{
		Debugging:      qwen,
		Testing:        qwen,
		Implementation: ornith,
		Architecture:   claude,
		Default:        qwen,
	})

	cases := []struct {
		taskType eval.TaskType
		want     string
	}{
		{eval.TaskDebugging, qwen.name},
		{eval.TaskTesting, qwen.name},
		{eval.TaskImplementation, ornith.name},
		{eval.TaskArchitecture, claude.name},
		{"unknown", qwen.name}, // falls back to primary
	}

	for _, tc := range cases {
		got := r.Select(tc.taskType).Name()
		if got != tc.want {
			t.Errorf("Select(%s) = %q, want %q", tc.taskType, got, tc.want)
		}
	}
}

func TestRouter_Generate(t *testing.T) {
	stub := &stubAdapter{name: "stub"}
	r := mustNew(t, router.Config{Default: stub})

	resp, err := r.Generate(context.Background(), eval.TaskDebugging, "fix this bug")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Text != "ok" {
		t.Errorf("got text %q, want %q", resp.Text, "ok")
	}
	if resp.Model != "stub" {
		t.Errorf("got model %q, want %q", resp.Model, "stub")
	}
}

func TestRouter_NilFallsBackToDefault(t *testing.T) {
	def := &stubAdapter{name: "default"}
	r := mustNew(t, router.Config{
		Default: def,
		// all nil → should fall back to default
	})

	for _, tt := range []eval.TaskType{
		eval.TaskDebugging, eval.TaskImplementation,
		eval.TaskArchitecture, eval.TaskTesting,
	} {
		if r.Select(tt).Name() != "default" {
			t.Errorf("Select(%s) should fall back to default", tt)
		}
	}
}

func TestOllamaAdapter_Name(t *testing.T) {
	// Tests OllamaAdapter.Name() without making a real HTTP call.
	a := router.NewOllamaAdapter(nil, "qwen2.5:7b")
	if a.Name() != "ollama/qwen2.5:7b" {
		t.Errorf("unexpected name: %s", a.Name())
	}
}

func TestClaudeAdapter_Name(t *testing.T) {
	a := router.NewClaudeAdapter("key", "claude-sonnet-4-6")
	if a.Name() != "claude/claude-sonnet-4-6" {
		t.Errorf("unexpected name: %s", a.Name())
	}
}

func TestClaudeAdapter_DefaultModel(t *testing.T) {
	a := router.NewClaudeAdapter("key", "")
	if a.Name() != "claude/claude-sonnet-4-6" {
		t.Errorf("default model mismatch: %s", a.Name())
	}
}
