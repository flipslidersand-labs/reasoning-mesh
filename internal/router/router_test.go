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

func TestRouter_SelectByTaskType(t *testing.T) {
	qwen := &stubAdapter{name: "ollama/qwen2.5:7b"}
	ornith := &stubAdapter{name: "ollama/ornith:9b"}
	claude := &stubAdapter{name: "claude/claude-sonnet-4-6"}

	r := router.New(router.Config{
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
	r := router.New(router.Config{Default: stub})

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
	r := router.New(router.Config{
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

func TestCodexAdapter_NotImplemented(t *testing.T) {
	a := router.NewCodexAdapter("")
	if a.Name() != "codex/gpt-4o" {
		t.Errorf("unexpected name: %s", a.Name())
	}
	_, err := a.Generate(context.Background(), "hello")
	if err == nil {
		t.Error("expected error from CodexAdapter.Generate")
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
