package knowledge_test

import (
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
)

func TestDeterministicID_Stable(t *testing.T) {
	// Same inputs must produce the same ID across calls.
	id1 := knowledge.ExportedDeterministicID(eval.TaskDebugging, "some content")
	id2 := knowledge.ExportedDeterministicID(eval.TaskDebugging, "some content")
	if id1 != id2 {
		t.Errorf("deterministic ID is not stable: %s vs %s", id1, id2)
	}
}

func TestDeterministicID_DifferentTaskType(t *testing.T) {
	id1 := knowledge.ExportedDeterministicID(eval.TaskDebugging, "content")
	id2 := knowledge.ExportedDeterministicID(eval.TaskImplementation, "content")
	if id1 == id2 {
		t.Error("different task types should produce different IDs")
	}
}

func TestDeterministicID_DifferentContent(t *testing.T) {
	id1 := knowledge.ExportedDeterministicID(eval.TaskDebugging, "content a")
	id2 := knowledge.ExportedDeterministicID(eval.TaskDebugging, "content b")
	if id1 == id2 {
		t.Error("different content should produce different IDs")
	}
}
