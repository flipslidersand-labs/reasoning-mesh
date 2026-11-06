package knowledge

import (
	"testing"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
)

func TestComputeScore_MissingTimestampNotZero(t *testing.T) {
	r := &QdrantRetriever{scorer: DefaultScorerConfig(), condition: eval.CondScore}
	recent := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)

	cases := map[string]map[string]any{
		"no timestamps":    {"usage_count": 1.0, "success_count": 1.0},
		"invalid lastUsed": {"usage_count": 1.0, "success_count": 1.0, "last_used_at": "garbage"},
		"created_at only":  {"usage_count": 1.0, "success_count": 1.0, "created_at": recent},
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			got := r.computeScore(qdrant.SearchResult{Score: 0.9, Payload: payload}, eval.TaskType("x"))
			if got <= 0 {
				t.Fatalf("score = %v, want > 0", got)
			}
		})
	}
}

func TestPayloadTime(t *testing.T) {
	if _, ok := payloadTime(map[string]any{}, "k"); ok {
		t.Error("missing key: ok = true")
	}
	if _, ok := payloadTime(map[string]any{"k": "bad"}, "k"); ok {
		t.Error("invalid: ok = true")
	}
	if _, ok := payloadTime(map[string]any{"k": "2026-01-02T03:04:05Z"}, "k"); !ok {
		t.Error("valid: ok = false")
	}
}
