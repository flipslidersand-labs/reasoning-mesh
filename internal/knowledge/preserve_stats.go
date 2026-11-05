package knowledge

import (
	"context"
	"fmt"
	"strings"

	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
)

// statsKeys are the feedback statistics owned by ScoreUpdater. Re-ingesting
// identical content (same deterministicID) must not reset them (#250).
var statsKeys = []string{"usage_count", "success_count", "success_rate", "created_at", "last_used_at"}

// preserveExistingStats fetches the points that already exist for pts' IDs
// (one GetByIDs call) and copies their statsKeys into the new payloads, so
// the subsequent full-point upsert only refreshes content/tags/etc.
// On lookup failure it returns an error and the caller must not upsert:
// overwriting would irreversibly erase learned scores, while skipping is
// recoverable by re-running the ingest.
func preserveExistingStats(ctx context.Context, qc *qdrant.Client, collection string, pts []qdrant.UpsertPoint) error {
	ids := make([]string, len(pts))
	for i, p := range pts {
		ids[i] = p.ID
	}
	existing, err := qc.GetByIDs(ctx, collection, ids)
	if err != nil {
		return fmt.Errorf("lookup existing points: %w", err)
	}
	byID := make(map[string]map[string]any, len(existing))
	for _, r := range existing {
		byID[normalizeID(r.ID)] = r.Payload
	}
	for _, p := range pts {
		old, ok := byID[normalizeID(p.ID)]
		if !ok {
			continue
		}
		for _, k := range statsKeys {
			if v, ok := old[k]; ok {
				p.Payload[k] = v
			}
		}
	}
	return nil
}

// normalizeID maps both our 32-hex IDs and Qdrant's dashed UUID form to one key.
func normalizeID(id string) string {
	return strings.ToLower(strings.ReplaceAll(id, "-", ""))
}
