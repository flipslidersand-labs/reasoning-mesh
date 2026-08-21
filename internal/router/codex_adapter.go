package router

import (
	"context"
	"fmt"
)

// CodexAdapter is a placeholder for future OpenAI Codex / GPT-4 integration.
type CodexAdapter struct {
	model string
}

// NewCodexAdapter creates a stub adapter.
func NewCodexAdapter(model string) *CodexAdapter {
	if model == "" {
		model = "gpt-4o"
	}
	return &CodexAdapter{model: model}
}

// Name returns the adapter identifier.
func (a *CodexAdapter) Name() string {
	return fmt.Sprintf("codex/%s", a.model)
}

// Generate always returns ErrNotImplemented.
func (a *CodexAdapter) Generate(_ context.Context, _ string) (Response, error) {
	return Response{}, fmt.Errorf("codex adapter: not implemented")
}
