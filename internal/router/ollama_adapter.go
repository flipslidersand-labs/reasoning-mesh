package router

import (
	"context"
	"fmt"

	"github.com/flipslidersand/reasoning-mesh/internal/ollama"
)

// OllamaAdapter wraps the Ollama HTTP client for a specific model.
type OllamaAdapter struct {
	client *ollama.Client
	model  string
}

// NewOllamaAdapter creates an adapter for the given Ollama model.
func NewOllamaAdapter(client *ollama.Client, model string) *OllamaAdapter {
	return &OllamaAdapter{client: client, model: model}
}

// Name returns a human-readable identifier.
func (a *OllamaAdapter) Name() string {
	return fmt.Sprintf("ollama/%s", a.model)
}

// Generate calls Ollama's /api/generate endpoint.
func (a *OllamaAdapter) Generate(ctx context.Context, prompt string) (Response, error) {
	full, err := a.client.GenerateFull(ctx, a.model, prompt)
	if err != nil {
		return Response{}, err
	}
	return Response{
		Text:         full.Response,
		PromptTokens: full.PromptEvalCount,
		TotalTokens:  full.PromptEvalCount + full.EvalCount,
		Model:        full.Model,
	}, nil
}
