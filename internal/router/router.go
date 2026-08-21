package router

import (
	"context"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
)

// Router selects a ModelAdapter based on the task type.
// Routing strategy:
//   - debugging / testing  → fast local model (qwen2.5:7b via Ollama)
//   - implementation       → code-focused local model (ornith:9b via Ollama)
//   - architecture         → high-reasoning model (Claude)
//   - default              → falls back to the primary adapter
type Router struct {
	adapters map[eval.TaskType]ModelAdapter
	primary  ModelAdapter
}

// Config holds per-task-type adapter assignments.
type Config struct {
	Debugging      ModelAdapter
	Implementation ModelAdapter
	Architecture   ModelAdapter
	Testing        ModelAdapter
	Default        ModelAdapter
}

// New creates a Router from the given config.
// Any nil adapter in cfg falls back to cfg.Default.
func New(cfg Config) *Router {
	resolve := func(a ModelAdapter) ModelAdapter {
		if a != nil {
			return a
		}
		return cfg.Default
	}

	return &Router{
		primary: cfg.Default,
		adapters: map[eval.TaskType]ModelAdapter{
			eval.TaskDebugging:      resolve(cfg.Debugging),
			eval.TaskImplementation: resolve(cfg.Implementation),
			eval.TaskArchitecture:   resolve(cfg.Architecture),
			eval.TaskTesting:        resolve(cfg.Testing),
		},
	}
}

// Select returns the adapter assigned to the given task type.
// Falls back to the primary adapter if the task type is unknown.
func (r *Router) Select(taskType eval.TaskType) ModelAdapter {
	if a, ok := r.adapters[taskType]; ok {
		return a
	}
	return r.primary
}

// Generate is a convenience method that selects the adapter and calls Generate.
func (r *Router) Generate(ctx context.Context, taskType eval.TaskType, prompt string) (Response, error) {
	return r.Select(taskType).Generate(ctx, prompt)
}
