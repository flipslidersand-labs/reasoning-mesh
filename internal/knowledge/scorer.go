package knowledge

import "math"

// ScorerConfig holds Beta distribution and freshness parameters. It is also the
// config.yaml `scorer:` schema, so config and runtime share one definition.
type ScorerConfig struct {
	Alpha           float64 `yaml:"alpha"`            // Beta prior alpha (default 2.0)
	Beta            float64 `yaml:"beta"`             // Beta prior beta  (default 2.0)
	FreshnessLambda float64 `yaml:"freshness_lambda"` // decay rate per day (default 0.01)
	TaskBoost       float64 `yaml:"task_boost"`       // multiplier when task_type matches (default 1.5)
}

func DefaultScorerConfig() ScorerConfig {
	return ScorerConfig{
		Alpha:           2.0,
		Beta:            2.0,
		FreshnessLambda: 0.01,
		TaskBoost:       1.5,
	}
}

// EffectiveSuccess returns the Beta-prior-smoothed success rate
// (success + α) / (usage + α + β). It is shared by Score() and
// ScoreUpdater.apply() so the stored success_rate and the retrieval score
// never diverge.
func (cfg ScorerConfig) EffectiveSuccess(usageCount, successCount int) float64 {
	return (float64(successCount) + cfg.Alpha) / (float64(usageCount) + cfg.Alpha + cfg.Beta)
}

// Score computes the final retrieval score for a knowledge item.
//
//	final_score = cosine_sim × sqrt(effective_success) × freshness × task_boost
//	effective_success = (success_count + α) / (usage_count + α + β)
//	freshness = exp(-λ × days_since_last_used)
func (cfg ScorerConfig) Score(cosineSim float64, usageCount, successCount int, daysSinceLastUsed float64, taskMatch bool) float64 {
	effectiveSuccess := cfg.EffectiveSuccess(usageCount, successCount)
	freshness := math.Exp(-cfg.FreshnessLambda * daysSinceLastUsed)

	boost := 1.0
	if taskMatch {
		boost = cfg.TaskBoost
	}

	return cosineSim * math.Sqrt(effectiveSuccess) * freshness * boost
}
