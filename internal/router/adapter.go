package router

import "context"

// Response holds the model output and token usage.
type Response struct {
	Text         string
	PromptTokens int
	TotalTokens  int
	Model        string
}

// ModelAdapter is the common interface for all LLM backends.
type ModelAdapter interface {
	// Generate sends a prompt and returns the model response.
	Generate(ctx context.Context, prompt string) (Response, error)
	// Name returns the adapter identifier (e.g. "ollama/qwen2.5:7b").
	Name() string
}
