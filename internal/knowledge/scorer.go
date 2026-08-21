package knowledge

import "math"

// ScorerConfig holds Beta distribution and freshness parameters.
type ScorerConfig struct {
	Alpha           float64 // Beta prior alpha (default 2.0)
	Beta            float64 // Beta prior beta  (default 2.0)
	FreshnessLambda float64 // decay rate per day (default 0.01)
	TaskBoost       float64 // multiplier when task_type matches (default 1.5)
}

func DefaultScorerConfig() ScorerConfig {
	return ScorerConfig{
		Alpha:           2.0,
		Beta:            2.0,
		FreshnessLambda: 0.01,
		TaskBoost:       1.5,
	}
}

// Score computes the final retrieval score for a knowledge item.
//
//	final_score = cosine_sim × sqrt(effective_success) × freshness × task_boost
//	effective_success = (success_count + α) / (usage_count + α + β)
//	freshness = exp(-λ × days_since_last_used)
func (cfg ScorerConfig) Score(cosineSim float64, usageCount, successCount int, daysSinceLastUsed float64, taskMatch bool) float64 {
	effectiveSuccess := (float64(successCount) + cfg.Alpha) / (float64(usageCount) + cfg.Alpha + cfg.Beta)
	freshness := math.Exp(-cfg.FreshnessLambda * daysSinceLastUsed)

	boost := 1.0
	if taskMatch {
		boost = cfg.TaskBoost
	}

	return cosineSim * math.Sqrt(effectiveSuccess) * freshness * boost
}
