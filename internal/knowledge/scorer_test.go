package knowledge_test

import (
	"math"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
)

func TestScorer_EffectiveSuccess(t *testing.T) {
	cfg := knowledge.DefaultScorerConfig() // α=2, β=2

	cases := []struct {
		name         string
		usage, succ  int
		wantApprox   float64
		tolerance    float64
	}{
		// 0/0 → effective = (0+2)/(0+2+2) = 0.5, sqrt(0.5)≈0.707
		{"cold start 0/0", 0, 0, 0.707, 0.01},
		// 1/1 → (1+2)/(1+4) = 0.6, sqrt(0.6)≈0.775
		{"one success", 1, 1, 0.775, 0.01},
		// 50/50 → (52)/(54) ≈ 0.963, sqrt≈0.981
		{"many successes", 50, 50, 0.981, 0.01},
		// 0/10 → (2)/(14) ≈ 0.143, sqrt≈0.378
		{"all failures", 10, 0, 0.378, 0.01},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// cosine=1, freshness=1 (days=0), no task boost → isolates effective_success component
			score := cfg.Score(1.0, tc.usage, tc.succ, 0, false)
			got := score // = sqrt(effective_success) since cosine=1, freshness=1, boost=1
			if math.Abs(got-tc.wantApprox) > tc.tolerance {
				t.Errorf("Score()=%.4f, want ≈%.4f (±%.4f)", got, tc.wantApprox, tc.tolerance)
			}
		})
	}
}

func TestScorer_Freshness(t *testing.T) {
	cfg := knowledge.DefaultScorerConfig() // λ=0.01

	// cosine=1, usage=50/50 (near-1 effective), no boost
	// freshness = exp(-0.01 * days)
	cases := []struct{ days, wantFreshness float64 }{
		{0, 1.0},
		{100, math.Exp(-1.0)},  // ≈ 0.368
		{365, math.Exp(-3.65)}, // ≈ 0.026
	}

	for _, tc := range cases {
		score := cfg.Score(1.0, 50, 50, tc.days, false)
		// score ≈ sqrt(52/54) * freshness ≈ 0.981 * freshness
		wantApprox := 0.981 * tc.wantFreshness
		if math.Abs(score-wantApprox) > 0.02 {
			t.Errorf("days=%.0f: score=%.4f, want ≈%.4f", tc.days, score, wantApprox)
		}
	}
}

func TestScorer_TaskBoost(t *testing.T) {
	cfg := knowledge.DefaultScorerConfig()

	withBoost := cfg.Score(0.8, 10, 8, 0, true)
	withoutBoost := cfg.Score(0.8, 10, 8, 0, false)

	ratio := withBoost / withoutBoost
	if math.Abs(ratio-cfg.TaskBoost) > 0.001 {
		t.Errorf("task boost ratio=%.4f, want %.4f", ratio, cfg.TaskBoost)
	}
}
