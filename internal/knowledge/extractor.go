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

const defaultUpsertBatchSize = 32

// Extractor is the full pipeline: chunk → structure → dedup ID → embed → upsert.
type Extractor struct {
	chunker        func(diff, ciLog string) []Chunk
	structurizer   *Structurizer
	embedder       *Embedder
	qdrant         *qdrant.Client
	collection     string
	upsertBatchSize int
}

// NewExtractor wires the full pipeline with the default batch size (32).
func NewExtractor(s *Structurizer, emb *Embedder, qc *qdrant.Client, collection string) *Extractor {
	return NewExtractorWithBatchSize(s, emb, qc, collection, defaultUpsertBatchSize)
}

// NewExtractorWithBatchSize wires the full pipeline with a configurable batch size.
// batchSize controls how many chunks are embed+upserted per HTTP round-trip.
// Use a smaller value to reduce per-request memory; use a larger value to reduce
// the number of round-trips (bounded by the Qdrant client timeout).
func NewExtractorWithBatchSize(s *Structurizer, emb *Embedder, qc *qdrant.Client, collection string, batchSize int) *Extractor {
	if batchSize <= 0 {
		batchSize = defaultUpsertBatchSize
	}
	return &Extractor{
		chunker: func(diff, ciLog string) []Chunk {
			chunks := ChunkDiff(diff)
			chunks = append(chunks, ChunkCILog(ciLog)...)
			return chunks
		},
		structurizer:    s,
		embedder:        emb,
		qdrant:          qc,
		collection:      collection,
		upsertBatchSize: batchSize,
	}
}

// Run extracts knowledge from a CI-green commit and upserts it to Qdrant.
// commitSHA is embedded in the deterministic ID to allow re-runs without duplication.
// Chunks are processed in batches of upsertBatchSize to bound memory usage and
// prevent HTTP timeout on large diffs.
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

	upserted := 0
	skipped := 0
	now := time.Now().UTC()

	for batchStart := 0; batchStart < len(chunks); batchStart += e.upsertBatchSize {
		batchEnd := batchStart + e.upsertBatchSize
		if batchEnd > len(chunks) {
			batchEnd = len(chunks)
		}
		batch := chunks[batchStart:batchEnd]

		// Structurize all chunks in this batch first, skipping failures.
		type structuredItem struct {
			sc  StructuredChunk
			id  string
		}
		items := make([]structuredItem, 0, len(batch))
		texts := make([]string, 0, len(batch))
		for _, chunk := range batch {
			sc, err := e.structurizer.Structurize(ctx, chunk)
			if err != nil {
				log.Printf("extractor: structurize error: %v", err)
				skipped++
				continue
			}
			items = append(items, structuredItem{sc: sc, id: deterministicID(sc.TaskType, sc.Content)})
			texts = append(texts, sc.Content)
		}
		if len(items) == 0 {
			continue
		}

		// Embed all texts in this batch in one request.
		vectors, err := e.embedder.Embed(ctx, texts)
		if err != nil {
			log.Printf("extractor: embed error (batch %d-%d): %v", batchStart, batchEnd-1, err)
			skipped += len(items)
			continue
		}

		// Build UpsertPoints from valid embed results.
		pts := make([]qdrant.UpsertPoint, 0, len(items))
		for i, item := range items {
			if i >= len(vectors) || len(vectors[i]) == 0 {
				skipped++
				continue
			}
			pts = append(pts, qdrant.UpsertPoint{
				ID:     item.id,
				Vector: vectors[i],
				Payload: map[string]any{
					"content":       item.sc.Content,
					"task_type":     string(toQdrantTaskType(item.sc.TaskType)),
					"language":      item.sc.Language,
					"framework":     item.sc.Framework,
					"source":        "ci",
					"commit_sha":    commitSHA,
					"usage_count":   0,
					"success_count": 0,
					"success_rate":  0.5,
					"tags":          item.sc.Tags,
					"created_at":    now.Format(time.RFC3339),
					"last_used_at":  now.Format(time.RFC3339),
				},
			})
		}
		if len(pts) == 0 {
			continue
		}

		if err := e.qdrant.BulkUpsert(ctx, e.collection, pts); err != nil {
			log.Printf("extractor: bulk upsert error (batch %d-%d): %v", batchStart, batchEnd-1, err)
			span.SetStatus(codes.Error, err.Error())
			skipped += len(pts)
			continue
		}
		log.Printf("extractor: bulk upserted %d points (batch %d-%d, commit=%s)", len(pts), batchStart, batchEnd-1, commitSHA)
		upserted += len(pts)
	}

	span.SetAttributes(attribute.Int("upserted_count", upserted))
	span.SetAttributes(attribute.Int("skipped_count", skipped))
	if skipped > 0 && upserted == 0 {
		log.Printf("extractor: WARNING all %d chunks were skipped (commit=%s); check structurize/embed errors above", skipped, commitSHA)
	} else if skipped > 0 {
		log.Printf("extractor: %d/%d chunks skipped (commit=%s)", skipped, len(chunks), commitSHA)
	}
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
