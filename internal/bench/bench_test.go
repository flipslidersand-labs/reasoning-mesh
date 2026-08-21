package bench_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/bench"
	"github.com/flipslidersand/reasoning-mesh/internal/eval"
)

func writeRun(t *testing.T, dir string, runAt time.Time, summaries []eval.Summary) {
	t.Helper()
	rs := eval.RunSummary{RunAt: runAt, Summaries: summaries}
	data, _ := json.Marshal(rs)
	name := runAt.Format("20060102-150405") + ".json"
	if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_Empty(t *testing.T) {
	dir := t.TempDir()
	runs, err := bench.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Errorf("expected 0 runs, got %d", len(runs))
	}
}

func TestLoad_Sorted(t *testing.T) {
	dir := t.TempDir()
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	writeRun(t, dir, t2, nil)
	writeRun(t, dir, t1, nil)

	runs, _ := bench.Load(dir)
	if len(runs) != 2 {
		t.Fatalf("expected 2, got %d", len(runs))
	}
	if !runs[0].RunAt.Equal(t1) {
		t.Errorf("first run should be t1, got %v", runs[0].RunAt)
	}
}

func TestComputeDeltas_Improvement(t *testing.T) {
	dir := t.TempDir()
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	writeRun(t, dir, t1, []eval.Summary{
		{Model: "qwen2.5:7b", Condition: eval.CondNoRAG, AvgRecall: 0.5, AvgAccuracy: 0.4, AvgLatencyMS: 1000},
		{Model: "qwen2.5:7b", Condition: eval.CondScore, AvgRecall: 0.6, AvgAccuracy: 0.5, AvgLatencyMS: 1100},
	})
	writeRun(t, dir, t2, []eval.Summary{
		{Model: "qwen2.5:7b", Condition: eval.CondNoRAG, AvgRecall: 0.55, AvgAccuracy: 0.42, AvgLatencyMS: 950},
		{Model: "qwen2.5:7b", Condition: eval.CondScore, AvgRecall: 0.75, AvgAccuracy: 0.65, AvgLatencyMS: 1150},
	})

	runs, _ := bench.Load(dir)
	deltas := bench.ComputeDeltas(runs)

	if len(deltas) != 2 {
		t.Fatalf("expected 2 deltas, got %d", len(deltas))
	}

	for _, d := range deltas {
		if d.Model != "qwen2.5:7b" {
			t.Errorf("unexpected model: %s", d.Model)
		}
		if d.Condition == eval.CondScore {
			if d.RecallDelta < 0 {
				t.Errorf("score condition recall should improve, got %.3f", d.RecallDelta)
			}
		}
	}
}

func TestComputeDeltas_TooFewRuns(t *testing.T) {
	dir := t.TempDir()
	writeRun(t, dir, time.Now(), nil)
	runs, _ := bench.Load(dir)
	deltas := bench.ComputeDeltas(runs)
	if deltas != nil {
		t.Error("should return nil for <2 runs")
	}
}
