package bench

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
)

func runWith(day int, acc float64) RunEntry {
	return RunEntry{
		RunAt: time.Date(2026, 3, day, 0, 0, 0, 0, time.UTC),
		Summaries: []eval.Summary{
			{Model: "m", Condition: eval.Condition("c"), AvgRecall: 0.5, AvgAccuracy: acc, AvgLatencyMS: 100},
		},
	}
}

func TestComputeDeltas_UngradedIsNaN(t *testing.T) {
	for _, accs := range [][2]float64{{-1, 0.5}, {0.5, -1}, {-1, -1}} {
		d := ComputeDeltas([]RunEntry{runWith(1, accs[0]), runWith(2, accs[1])})
		if len(d) != 1 || !math.IsNaN(d[0].AccuracyDelta) {
			t.Fatalf("accs=%v: want NaN, got %+v", accs, d)
		}
	}
	d := ComputeDeltas([]RunEntry{runWith(1, 0.5), runWith(2, 0.5)})
	if d[0].AccuracyDelta != 0 {
		t.Fatalf("graded equal runs should be 0, got %v", d[0].AccuracyDelta)
	}
}

func TestFprintDeltas_NA(t *testing.T) {
	runs := []RunEntry{runWith(1, -1), runWith(2, 0.5)}
	var buf bytes.Buffer
	FprintDeltas(&buf, runs, ComputeDeltas(runs))
	out := buf.String()
	if !strings.Contains(out, "n/a") || strings.Contains(out, "NaN") {
		t.Fatalf("want n/a without NaN:\n%s", out)
	}
	if !strings.Contains(out, "2 runs: 2026-03-01 → 2026-03-02") {
		t.Fatalf("header missing:\n%s", out)
	}
}

func TestFprintDeltas_Graded(t *testing.T) {
	runs := []RunEntry{runWith(1, 0.5), runWith(2, 0.75)}
	var buf bytes.Buffer
	FprintDeltas(&buf, runs, ComputeDeltas(runs))
	if !strings.Contains(buf.String(), "+0.250") || strings.Contains(buf.String(), "n/a") {
		t.Fatalf("unexpected:\n%s", buf.String())
	}
}

func TestFprintDeltas_TooFewRuns(t *testing.T) {
	var buf bytes.Buffer
	FprintDeltas(&buf, []RunEntry{runWith(1, 0.5)}, nil)
	if !strings.Contains(buf.String(), "need ≥2 runs") {
		t.Fatalf("got %q", buf.String())
	}
}

func TestFmtDelta(t *testing.T) {
	negZero := math.Copysign(0, -1)
	cases := []struct {
		in   float64
		want string
	}{{0.25, "+0.250"}, {-0.25, "-0.250"}, {0, "+0.000"}, {negZero, "+0.000"}}
	for _, c := range cases {
		if got := fmtDelta(c.in); got != c.want {
			t.Errorf("fmtDelta(%v)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestFmtLatency(t *testing.T) {
	negZero := math.Copysign(0, -1)
	cases := []struct {
		in   float64
		want string
	}{{12, "+12ms"}, {-12, "-12ms"}, {0, "+0ms"}, {negZero, "+0ms"}}
	for _, c := range cases {
		if got := fmtLatency(c.in); got != c.want {
			t.Errorf("fmtLatency(%v)=%q want %q", c.in, got, c.want)
		}
	}
}
