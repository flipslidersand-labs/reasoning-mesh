package knowledge_test

import (
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
)

func TestExperienceID_Stable(t *testing.T) {
	id1 := knowledge.ExportedExperienceID(eval.TaskDebugging, qdrant.FailureTest, "content")
	id2 := knowledge.ExportedExperienceID(eval.TaskDebugging, qdrant.FailureTest, "content")
	if id1 != id2 {
		t.Errorf("experience ID not stable: %s vs %s", id1, id2)
	}
}

func TestExperienceID_DifferentFailureType(t *testing.T) {
	id1 := knowledge.ExportedExperienceID(eval.TaskDebugging, qdrant.FailureTest, "c")
	id2 := knowledge.ExportedExperienceID(eval.TaskDebugging, qdrant.FailureCompile, "c")
	if id1 == id2 {
		t.Error("different failure types should produce different IDs")
	}
}
