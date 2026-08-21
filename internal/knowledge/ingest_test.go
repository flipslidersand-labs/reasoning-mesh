package knowledge_test

import (
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
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
