package eval

import "testing"

// #270: out-of-range or non-numeric judge output must be "not scored" (-1),
// never clamped into a score that feeds model comparison and /v1/feedback.
func TestParseJudgeScore(t *testing.T) {
	tests := []struct {
		raw  string
		want float64
	}{
		{"0.8", 0.8},
		{"0.8\n", 0.8},
		{"  1.0 \n", 1.0},
		{"0", 0},
		{"1", 1},
		{"0.5", 0.5},
		{"8", -1},
		{"85", -1},
		{"7/10", -1},
		{"1.0ではない", -1},
		{"1.0ではありません", -1},
		{"-0.2", -1},
		{"1.01", -1},
		{"NaN", -1},
		{"Inf", -1},
		{"", -1},
		{"abc", -1},
	}
	for _, tt := range tests {
		if got := parseJudgeScore(tt.raw); got != tt.want {
			t.Errorf("parseJudgeScore(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}
}
