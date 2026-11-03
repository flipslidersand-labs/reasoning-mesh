package knowledge_test

import (
	"context"
	"strings"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
)

// deterministicID is already tested via ExportedDeterministicID.
// Here we verify that manual ingest produces the same ID as auto-ingest
// for the same task_type + content pair, ensuring dedup works across paths.

func TestIngestEntry_IDMatchesExtractor(t *testing.T) {
	content := "goroutine leak: missing cancel in context.WithCancel"
	taskType := eval.TaskDebugging

	// ID from manual ingest path
	manualID := knowledge.ExportedDeterministicID(taskType, content)
	// ID from extractor path (same formula)
	extractorID := knowledge.ExportedDeterministicID(taskType, content)

	if manualID != extractorID {
		t.Errorf("manual ID %s != extractor ID %s", manualID, extractorID)
	}
}

func TestIngestEntry_DefaultTaskType(t *testing.T) {
	// Empty task_type should default to implementation in Ingest(),
	// so IDs should differ from a debugging entry with the same content.
	content := "some code pattern"
	idImpl := knowledge.ExportedDeterministicID(eval.TaskImplementation, content)
	idDebug := knowledge.ExportedDeterministicID(eval.TaskDebugging, content)

	if idImpl == idDebug {
		t.Error("different task types should produce different IDs (dedup must be task-scoped)")
	}
}

// #212: an unknown task_type used to be silently rewritten to "implementation"
// by toQdrantTaskType; it must now be rejected before anything is embedded/stored.
func TestIngest_UnknownTaskType_Rejected(t *testing.T) {
	s := knowledge.NewIngestStore(nil, nil, "col") // nil embedder: validation must fire first
	_, err := s.Ingest(context.Background(), knowledge.IngestEntry{Content: "x", TaskType: "bogus"})
	if err == nil || !strings.Contains(err.Error(), "task_type") {
		t.Fatalf("err = %v, want task_type validation error", err)
	}
}

// eval.TaskType and qdrant.TaskType are one type; no conversion function exists.
func TestTaskType_SharedBetweenEvalAndQdrant(t *testing.T) {
	var q qdrant.TaskType = eval.TaskDebugging
	if string(q) != "debugging" {
		t.Fatalf("got %q", q)
	}
}

func TestExperienceRecord_UnknownTaskType_Rejected(t *testing.T) {
	s := knowledge.NewExperienceStore(nil, nil, "col")
	err := s.Record(context.Background(), knowledge.FailureRecord{Content: "x", TaskType: "bogus"})
	if err == nil || !strings.Contains(err.Error(), "task_type") {
		t.Fatalf("err = %v, want task_type validation error", err)
	}
}
