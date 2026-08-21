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

// Extractor is the full pipeline: chunk → structure → dedup ID → embed → upsert.
type Extractor struct {
	chunker      func(diff, ciLog string) []Chunk
	structurizer *Structurizer
	embedder     *Embedder
	qdrant       *qdrant.Client
	collection   string
}

// NewExtractor wires the full pipeline.
func NewExtractor(s *Structurizer, emb *Embedder, qc *qdrant.Client, collection string) *Extractor {
	return &Extractor{
		chunker: func(diff, ciLog string) []Chunk {
			chunks := ChunkDiff(diff)
			chunks = append(chunks, ChunkCILog(ciLog)...)
			return chunks
		},
		structurizer: s,
		embedder:     emb,
		qdrant:       qc,
		collection:   collection,
	}
}

// Run extracts knowledge from a CI-green commit and upserts it to Qdrant.
// commitSHA is embedded in the deterministic ID to allow re-runs without duplication.
func (e *Extractor) Run(ctx context.Context, commitSHA, diff, ciLog string) error {
	chunks := e.chunker(diff, ciLog)
	if len(chunks) == 0 {
		log.Printf("extractor: no chunks from commit %s", commitSHA)
		return nil
	}

	for _, chunk := range chunks {
		sc, err := e.structurizer.Structurize(ctx, chunk)
		if err != nil {
			log.Printf("extractor: structurize error: %v", err)
			continue
		}

		id := deterministicID(sc.TaskType, sc.Content)

		vectors, err := e.embedder.Embed(ctx, []string{sc.Content})
		if err != nil {
			log.Printf("extractor: embed error: %v", err)
			continue
		}
		if len(vectors) == 0 || len(vectors[0]) == 0 {
			continue
		}

		now := time.Now().UTC()
		payload := map[string]any{
			"content":       sc.Content,
			"task_type":     string(toQdrantTaskType(sc.TaskType)),
			"language":      sc.Language,
			"framework":     sc.Framework,
			"source":        "ci",
			"commit_sha":    commitSHA,
			"usage_count":   0,
			"success_count": 0,
			"success_rate":  0.5,
			"tags":          sc.Tags,
			"created_at":    now.Format(time.RFC3339),
			"last_used_at":  now.Format(time.RFC3339),
		}

		if err := e.qdrant.Upsert(ctx, e.collection, id, vectors[0], payload); err != nil {
			log.Printf("extractor: upsert %s error: %v", id, err)
			continue
		}
		log.Printf("extractor: upserted %s (task=%s lang=%s)", id, sc.TaskType, sc.Language)
	}
	return nil
}

// ExportedDeterministicID is deterministicID exposed for testing.
var ExportedDeterministicID = deterministicID

// deterministicID returns sha256(task_type + "|" + content_hash) as a hex string.
// This ensures idempotent upserts: re-ingesting the same content is a no-op.
func deterministicID(taskType eval.TaskType, content string) string {
	contentHash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	combined := string(taskType) + "|" + contentHash
	return fmt.Sprintf("%x", sha256.Sum256([]byte(combined)))
}

func toQdrantTaskType(t eval.TaskType) qdrant.TaskType {
	switch t {
	case eval.TaskDebugging:
		return qdrant.TaskDebugging
	case eval.TaskImplementation:
		return qdrant.TaskImplementation
	case eval.TaskArchitecture:
		return qdrant.TaskArchitecture
	case eval.TaskTesting:
		return qdrant.TaskTesting
	default:
		return qdrant.TaskImplementation
	}
}
