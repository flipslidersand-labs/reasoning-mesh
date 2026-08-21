// Package bench provides longitudinal comparison of eval run results.
// It reads N RunSummary JSON files (sorted by run_at) and computes
// deltas per model×condition to track improvement over time.
package bench

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
)

// RunEntry is one data point in the longitudinal series.
type RunEntry struct {
	RunAt     time.Time
	Summaries []eval.Summary
	File      string
}

// Delta is the change between two runs for a specific model×condition.
type Delta struct {
	Model         string
	Condition     eval.Condition
	RecallDelta   float64 // positive = improvement
	AccuracyDelta float64 // positive = improvement
	LatencyDelta  float64 // negative = improvement
	RunCount      int     // number of runs in the series
}

// Load reads all *.json files in dir and returns them sorted by run_at ascending.
func Load(dir string) ([]RunEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("bench load: %w", err)
	}

	var runs []RunEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var rs eval.RunSummary
		if err := json.Unmarshal(data, &rs); err != nil {
			continue
		}
		runs = append(runs, RunEntry{RunAt: rs.RunAt, Summaries: rs.Summaries, File: e.Name()})
	}

	sort.Slice(runs, func(i, j int) bool {
		return runs[i].RunAt.Before(runs[j].RunAt)
	})
	return runs, nil
}

// ComputeDeltas returns the change from the first run to the last run per model×condition.
func ComputeDeltas(runs []RunEntry) []Delta {
	if len(runs) < 2 {
		return nil
	}

	type key struct {
		Model     string
		Condition eval.Condition
	}

	first := indexByKey(runs[0].Summaries)
	last := indexByKey(runs[len(runs)-1].Summaries)

	var deltas []Delta
	for k, lastS := range last {
		firstS, ok := first[k]
		if !ok {
			continue
		}
		recallDelta := lastS.AvgRecall - firstS.AvgRecall
		accDelta := 0.0
		if lastS.AvgAccuracy >= 0 && firstS.AvgAccuracy >= 0 {
			accDelta = lastS.AvgAccuracy - firstS.AvgAccuracy
		}
		latencyDelta := lastS.AvgLatencyMS - firstS.AvgLatencyMS

		deltas = append(deltas, Delta{
			Model:         k.Model,
			Condition:     k.Condition,
			RecallDelta:   recallDelta,
			AccuracyDelta: accDelta,
			LatencyDelta:  latencyDelta,
			RunCount:      len(runs),
		})
	}

	sort.Slice(deltas, func(i, j int) bool {
		if deltas[i].Model != deltas[j].Model {
			return deltas[i].Model < deltas[j].Model
		}
		return string(deltas[i].Condition) < string(deltas[j].Condition)
	})
	return deltas
}

// PrintDeltas writes a formatted delta table to stdout.
func PrintDeltas(runs []RunEntry, deltas []Delta) {
	if len(runs) < 2 {
		fmt.Println("longitudinal bench: need ≥2 runs, nothing to compare")
		return
	}

	first := runs[0]
	last := runs[len(runs)-1]
	fmt.Printf("=== Longitudinal Benchmark (%d runs: %s → %s) ===\n",
		len(runs),
		first.RunAt.Format("2006-01-02"),
		last.RunAt.Format("2006-01-02"),
	)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "MODEL\tCONDITION\tRECALL_Δ\tACCURACY_Δ\tLATENCY_Δ")
	fmt.Fprintln(w, "-----\t---------\t--------\t----------\t---------")
	for _, d := range deltas {
		accStr := "n/a"
		if !math.IsNaN(d.AccuracyDelta) {
			accStr = fmtDelta(d.AccuracyDelta)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			d.Model, d.Condition,
			fmtDelta(d.RecallDelta),
			accStr,
			fmtLatency(d.LatencyDelta),
		)
	}
	w.Flush()
}

func fmtDelta(v float64) string {
	sign := "+"
	if v < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.3f", sign, v)
}

func fmtLatency(v float64) string {
	sign := "+"
	if v < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.0fms", sign, v)
}

func indexByKey(summaries []eval.Summary) map[struct{ Model string; Condition eval.Condition }]eval.Summary {
	m := map[struct{ Model string; Condition eval.Condition }]eval.Summary{}
	for _, s := range summaries {
		m[struct{ Model string; Condition eval.Condition }{s.Model, s.Condition}] = s
	}
	return m
}
