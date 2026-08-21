package knowledge

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
)

// IngestEntry is a manually authored knowledge item.
type IngestEntry struct {
	Content   string
	TaskType  eval.TaskType
	Language  string
	Framework string
	Tags      []string
}

// IngestStore writes manually authored entries directly to the knowledge collection.
type IngestStore struct {
	embedder   *Embedder
	qdrant     *qdrant.Client
	collection string
}

// NewIngestStore creates an IngestStore.
func NewIngestStore(emb *Embedder, qc *qdrant.Client, collection string) *IngestStore {
	return &IngestStore{
		embedder:   emb,
		qdrant:     qc,
		collection: collection,
	}
}

// Ingest embeds a single entry and upserts it to Qdrant.
// The deterministic ID is the same formula used by the Extractor
// (sha256 of task_type + "|" + content_hash), so a manual entry
// that duplicates an auto-ingested one safely overwrites it.
func (s *IngestStore) Ingest(ctx context.Context, entry IngestEntry) (string, error) {
	if entry.Content == "" {
		return "", fmt.Errorf("content must not be empty")
	}
	if entry.TaskType == "" {
		entry.TaskType = eval.TaskImplementation
	}

	vectors, err := s.embedder.Embed(ctx, []string{entry.Content})
	if err != nil {
		return "", fmt.Errorf("embed: %w", err)
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return "", fmt.Errorf("embed: empty vector")
	}

	id := deterministicID(entry.TaskType, entry.Content)
	now := time.Now().UTC()
	payload := map[string]any{
		"content":       entry.Content,
		"task_type":     string(toQdrantTaskType(entry.TaskType)),
		"language":      entry.Language,
		"framework":     entry.Framework,
		"source":        "manual",
		"usage_count":   0,
		"success_count": 0,
		"success_rate":  0.5,
		"tags":          entry.Tags,
		"created_at":    now.Format(time.RFC3339),
		"last_used_at":  now.Format(time.RFC3339),
	}

	if err := s.qdrant.Upsert(ctx, s.collection, id, vectors[0], payload); err != nil {
		return "", fmt.Errorf("upsert: %w", err)
	}
	log.Printf("ingest: upserted %s (task=%s lang=%s)", id, entry.TaskType, entry.Language)
	return id, nil
}
