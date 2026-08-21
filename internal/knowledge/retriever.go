package knowledge

import (
	"context"
	"fmt"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
)

// QdrantRetriever implements eval.Retriever using Qdrant + e5 embeddings.
type QdrantRetriever struct {
	qdrant     *qdrant.Client
	embedder   *Embedder
	collection string
	scorer     ScorerConfig
	condition  eval.Condition
}

func NewQdrantRetriever(
	qdrantClient *qdrant.Client,
	embedder *Embedder,
	collection string,
	scorer ScorerConfig,
	condition eval.Condition,
) *QdrantRetriever {
	return &QdrantRetriever{
		qdrant:     qdrantClient,
		embedder:   embedder,
		collection: collection,
		scorer:     scorer,
		condition:  condition,
	}
}

func (r *QdrantRetriever) Retrieve(ctx context.Context, query string, taskType eval.TaskType, topK int) ([]eval.KnowledgeItem, error) {
	vec, err := r.embedder.EmbedOne(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// Optional task_type filter
	var filter map[string]any
	if taskType != "" {
		filter = map[string]any{
			"must": []map[string]any{
				{"key": "task_type", "match": map[string]any{"value": string(taskType)}},
			},
		}
	}

	// Fetch more candidates for reranking in score/compressed conditions
	fetchK := topK
	if r.condition == eval.CondScore {
		fetchK = topK * 3
	}

	hits, err := r.qdrant.Search(ctx, r.collection, vec, fetchK, filter)
	if err != nil {
		return nil, fmt.Errorf("qdrant search: %w", err)
	}

	items := make([]eval.KnowledgeItem, 0, len(hits))
	for _, hit := range hits {
		content, _ := hit.Payload["content"].(string)
		item := eval.KnowledgeItem{
			ID:      hit.ID,
			Content: content,
			Score:   r.computeScore(hit, taskType),
		}
		items = append(items, item)
	}

	// Rerank by score for CondScore condition
	if r.condition == eval.CondScore {
		sortByScore(items)
		if len(items) > topK {
			items = items[:topK]
		}
	}

	return items, nil
}

func (r *QdrantRetriever) computeScore(hit qdrant.SearchResult, taskType eval.TaskType) float64 {
	if r.condition == eval.CondCosine {
		return hit.Score
	}

	// Beta-distribution reranking for CondScore
	usageCount := payloadInt(hit.Payload, "usage_count")
	successCount := payloadInt(hit.Payload, "success_count")
	lastUsedAt := payloadTime(hit.Payload, "last_used_at")
	days := time.Since(lastUsedAt).Hours() / 24
	hitTaskType := payloadStr(hit.Payload, "task_type")
	taskMatch := hitTaskType == string(taskType)

	return r.scorer.Score(hit.Score, usageCount, successCount, days, taskMatch)
}

func sortByScore(items []eval.KnowledgeItem) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].Score > items[j-1].Score; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func payloadInt(p map[string]any, key string) int {
	v, _ := p[key].(float64)
	return int(v)
}

func payloadStr(p map[string]any, key string) string {
	v, _ := p[key].(string)
	return v
}

func payloadTime(p map[string]any, key string) time.Time {
	s, _ := p[key].(string)
	t, _ := time.Parse(time.RFC3339, s)
	return t
}
