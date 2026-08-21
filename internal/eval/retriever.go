package eval

import "context"

// KnowledgeItem is a retrieved knowledge chunk passed to the model as context.
type KnowledgeItem struct {
	ID      string
	Content string
	Score   float64
}

// Retriever retrieves relevant knowledge for a given query and task type.
// A stub (NoopRetriever) is used until Phase 2 (Qdrant retriever) is implemented.
type Retriever interface {
	Retrieve(ctx context.Context, query string, taskType TaskType, topK int) ([]KnowledgeItem, error)
}

// NoopRetriever returns empty results — used in no-rag condition and as a placeholder.
type NoopRetriever struct{}

func (NoopRetriever) Retrieve(_ context.Context, _ string, _ TaskType, _ int) ([]KnowledgeItem, error) {
	return nil, nil
}
