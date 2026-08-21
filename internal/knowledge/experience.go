package knowledge

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
)

// FailureRecord is the input to the Experience Store.
type FailureRecord struct {
	Content             string
	TaskType            eval.TaskType
	FailureType         qdrant.FailureType
	Evaluator           string  // "ci" | "human" | "llm"
	Confidence          float64 // 0.0–1.0
	RelatedKnowledgeIDs []string
}

// ExperienceStore writes failure records to the experience collection in Qdrant.
type ExperienceStore struct {
	embedder   *Embedder
	qdrant     *qdrant.Client
	collection string
}

// NewExperienceStore creates an ExperienceStore.
func NewExperienceStore(emb *Embedder, qc *qdrant.Client, collection string) *ExperienceStore {
	return &ExperienceStore{
		embedder:   emb,
		qdrant:     qc,
		collection: collection,
	}
}

// Record embeds the failure and upserts it to the experience collection.
// Idempotent: same content + task_type + failure_type → same ID.
func (s *ExperienceStore) Record(ctx context.Context, rec FailureRecord) error {
	vectors, err := s.embedder.Embed(ctx, []string{rec.Content})
	if err != nil {
		return fmt.Errorf("experience embed: %w", err)
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return fmt.Errorf("experience embed: empty vector")
	}

	id := experienceID(rec.TaskType, rec.FailureType, rec.Content)
	now := time.Now().UTC()
	payload := map[string]any{
		"content":               rec.Content,
		"task_type":             string(toQdrantTaskType(rec.TaskType)),
		"outcome_status":        string(qdrant.OutcomeFailure),
		"failure_type":          string(rec.FailureType),
		"evaluator":             rec.Evaluator,
		"confidence":            rec.Confidence,
		"related_knowledge_ids": rec.RelatedKnowledgeIDs,
		"created_at":            now.Format(time.RFC3339),
	}

	if err := s.qdrant.Upsert(ctx, s.collection, id, vectors[0], payload); err != nil {
		return fmt.Errorf("experience upsert: %w", err)
	}
	log.Printf("experience: recorded %s (task=%s failure=%s eval=%s)", id, rec.TaskType, rec.FailureType, rec.Evaluator)
	return nil
}

// ExportedExperienceID exposes experienceID for testing.
var ExportedExperienceID = experienceID

func experienceID(taskType eval.TaskType, failureType qdrant.FailureType, content string) string {
	contentHash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	combined := string(taskType) + "|" + string(failureType) + "|" + contentHash
	return fmt.Sprintf("%x", sha256.Sum256([]byte(combined)))
}
