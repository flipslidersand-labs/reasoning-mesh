package knowledge

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
	"github.com/flipslidersand/reasoning-mesh/internal/telemetry"
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
	ctx, span := telemetry.Tracer("knowledge/extractor").Start(ctx, "extractor.Run")
	defer span.End()
	span.SetAttributes(attribute.String("commit_sha", commitSHA))

	chunks := e.chunker(diff, ciLog)
	span.SetAttributes(attribute.Int("chunk_count", len(chunks)))
	if len(chunks) == 0 {
		log.Printf("extractor: no chunks from commit %s", commitSHA)
		return nil
	}

	var pts []qdrant.UpsertPoint
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

		pts = append(pts, qdrant.UpsertPoint{ID: id, Vectors: vectors[0], Payload: payload})
		log.Printf("extractor: queued %s (task=%s lang=%s)", id, sc.TaskType, sc.Language)
	}

	if err := e.qdrant.BulkUpsert(ctx, e.collection, pts); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("extractor: bulk upsert: %w", err)
	}
	upserted := len(pts)
	span.SetAttributes(attribute.Int("upserted_count", upserted))
	return nil
}

// ExportedDeterministicID is deterministicID exposed for testing.
var ExportedDeterministicID = deterministicID

// deterministicID returns a Qdrant-compatible UUID (32 lowercase hex chars) derived from
// sha256(task_type + "|" + sha256(content)). Qdrant requires UUID or uint64 as point ID.
func deterministicID(taskType eval.TaskType, content string) string {
	contentHash := sha256.Sum256([]byte(content))
	combined := string(taskType) + "|" + fmt.Sprintf("%x", contentHash)
	h := sha256.Sum256([]byte(combined))
	// Use first 16 bytes (128 bits) formatted as UUID hex (no dashes)
	return fmt.Sprintf("%x", h[:16])
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
