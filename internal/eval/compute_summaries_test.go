package eval

import (
	"math"
	"testing"
)

func csFind(t *testing.T, ss []Summary, model string, cond Condition) Summary {
	t.Helper()
	for _, s := range ss {
		if s.Model == model && s.Condition == cond {
			return s
		}
	}
	t.Fatalf("summary not found for %s/%s in %+v", model, cond, ss)
	return Summary{}
}

func csNear(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestComputeSummaries_ErrorExclusionAndAccuracyTable(t *testing.T) {
	tests := []struct {
		name    string
		results []Result
		want    Summary
	}{
		{
			name: "2 success + 1 error excludes error from averages",
			results: []Result{
				{Model: "m", Condition: CondCosine, LatencyMS: 100, PromptTokens: 10, CompletionTokens: 5, KeywordRecall: 1.0, Accuracy: 1.0},
				{Model: "m", Condition: CondCosine, LatencyMS: 300, PromptTokens: 20, CompletionTokens: 5, KeywordRecall: 0.5, Accuracy: 0.5},
				{Model: "m", Condition: CondCosine, LatencyMS: 9999, PromptTokens: 999, CompletionTokens: 999, KeywordRecall: 0, Accuracy: 0, Error: "boom"},
			},
			want: Summary{Count: 3, ErrorCount: 1, AvgLatencyMS: 200, TotalTokens: 40, AvgRecall: 0.75, AvgAccuracy: 0.75},
		},
		{
			name: "all errors yields zeros and finite values",
			results: []Result{
				{Model: "m", Condition: CondCosine, LatencyMS: 50, KeywordRecall: 1, Accuracy: 1, Error: "e1"},
				{Model: "m", Condition: CondCosine, LatencyMS: 70, KeywordRecall: 1, Accuracy: 1, Error: "e2"},
			},
			want: Summary{Count: 2, ErrorCount: 2, AvgLatencyMS: 0, TotalTokens: 0, AvgRecall: 0, AvgAccuracy: -1},
		},
		{
			name: "all unscored gives AvgAccuracy -1 but recall still averaged",
			results: []Result{
				{Model: "m", Condition: CondCosine, LatencyMS: 10, KeywordRecall: 1.0, Accuracy: -1},
				{Model: "m", Condition: CondCosine, LatencyMS: 30, KeywordRecall: 0.0, Accuracy: -1},
			},
			want: Summary{Count: 2, AvgLatencyMS: 20, AvgRecall: 0.5, AvgAccuracy: -1},
		},
		{
			name: "mixed scored and unscored averages only scored",
			results: []Result{
				{Model: "m", Condition: CondCosine, Accuracy: 1.0},
				{Model: "m", Condition: CondCosine, Accuracy: -1},
				{Model: "m", Condition: CondCosine, Accuracy: 0.5},
			},
			want: Summary{Count: 3, AvgAccuracy: 0.75},
		},
		{
			name: "accuracy 0 is scored, not unscored",
			results: []Result{
				{Model: "m", Condition: CondCosine, Accuracy: 0},
				{Model: "m", Condition: CondCosine, Accuracy: 1.0},
			},
			want: Summary{Count: 2, AvgAccuracy: 0.5},
		},
		{
			name: "errored scored result does not contribute accuracy",
			results: []Result{
				{Model: "m", Condition: CondCosine, Accuracy: 0.2},
				{Model: "m", Condition: CondCosine, Accuracy: 1.0, Error: "x"},
			},
			want: Summary{Count: 2, ErrorCount: 1, AvgAccuracy: 0.2},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ss := ComputeSummaries(tc.results)
			if len(ss) != 1 {
				t.Fatalf("len = %d, want 1", len(ss))
			}
			got := ss[0]
			for name, v := range map[string]float64{"latency": got.AvgLatencyMS, "recall": got.AvgRecall, "acc": got.AvgAccuracy} {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Fatalf("%s is not finite: %v", name, v)
				}
			}
			if got.Count != tc.want.Count || got.ErrorCount != tc.want.ErrorCount ||
				got.TotalTokens != tc.want.TotalTokens {
				t.Errorf("counts got %+v, want %+v", got, tc.want)
			}
			if !csNear(got.AvgLatencyMS, tc.want.AvgLatencyMS) || !csNear(got.AvgRecall, tc.want.AvgRecall) || !csNear(got.AvgAccuracy, tc.want.AvgAccuracy) {
				t.Errorf("averages got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestComputeSummaries_SeparatesModelAndConditionKeys(t *testing.T) {
	results := []Result{
		{Model: "a", Condition: CondNoRAG, LatencyMS: 100, KeywordRecall: 1, Accuracy: 1},
		{Model: "a", Condition: CondCosine, LatencyMS: 200, KeywordRecall: 0.5, Accuracy: 0.5},
		{Model: "b", Condition: CondNoRAG, LatencyMS: 300, KeywordRecall: 0, Accuracy: 0},
		{Model: "a", Condition: CondNoRAG, LatencyMS: 300, KeywordRecall: 0, Accuracy: 0},
		{Model: "b", Condition: CondCosine, Error: "e"},
	}
	ss := ComputeSummaries(results)
	if len(ss) != 4 {
		t.Fatalf("len = %d, want 4: %+v", len(ss), ss)
	}
	// order-independent lookups
	a := csFind(t, ss, "a", CondNoRAG)
	if a.Count != 2 || !csNear(a.AvgLatencyMS, 200) || !csNear(a.AvgRecall, 0.5) || !csNear(a.AvgAccuracy, 0.5) {
		t.Errorf("a/no-rag = %+v", a)
	}
	ac := csFind(t, ss, "a", CondCosine)
	if ac.Count != 1 || !csNear(ac.AvgLatencyMS, 200) || !csNear(ac.AvgRecall, 0.5) || !csNear(ac.AvgAccuracy, 0.5) {
		t.Errorf("a/cosine = %+v", ac)
	}
	b := csFind(t, ss, "b", CondNoRAG)
	if b.Count != 1 || !csNear(b.AvgLatencyMS, 300) || b.ErrorCount != 0 || b.AvgAccuracy != 0 {
		t.Errorf("b/no-rag = %+v", b)
	}
	bc := csFind(t, ss, "b", CondCosine)
	if bc.Count != 1 || bc.ErrorCount != 1 || bc.AvgAccuracy != -1 {
		t.Errorf("b/cosine = %+v", bc)
	}
}

func TestComputeSummaries_EmptyInput(t *testing.T) {
	if ss := ComputeSummaries(nil); len(ss) != 0 {
		t.Errorf("len = %d, want 0", len(ss))
	}
}
