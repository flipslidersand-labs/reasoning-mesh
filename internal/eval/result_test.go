package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeywordRecall_CommaSynonymEitherMatches(t *testing.T) {
	var r Result

	got := r.keywordRecall("この設計はテスト可能です", []string{"testable,テスト可能"})
	if got != 1.0 {
		t.Errorf("recall = %.2f, want 1.0 (Japanese alternative should match)", got)
	}

	got = r.keywordRecall("this design is testable", []string{"testable,テスト可能"})
	if got != 1.0 {
		t.Errorf("recall = %.2f, want 1.0 (English alternative should match)", got)
	}

	got = r.keywordRecall("this design is flexible", []string{"testable,テスト可能"})
	if got != 0.0 {
		t.Errorf("recall = %.2f, want 0.0 (neither alternative present)", got)
	}
}

func TestKeywordRecall_CommaSynonymDoesNotInflateDenominator(t *testing.T) {
	var r Result

	// A synonym group still counts as exactly one required keyword.
	got := r.keywordRecall("testable interface config", []string{"config", "testable,テスト可能", "interface,インターフェース"})
	if got != 1.0 {
		t.Errorf("recall = %.2f, want 1.0 (3 keywords, all matched via one alternative each)", got)
	}
}

func TestSaveRunJSON_RAGDegradedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	for _, degraded := range []bool{true, false} {
		p := filepath.Join(dir, fmt.Sprintf("d%v.json", degraded))
		if err := SaveRunJSON(nil, p, degraded); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(p)
		var rs RunSummary
		if err := json.Unmarshal(data, &rs); err != nil {
			t.Fatal(err)
		}
		if rs.RAGDegraded != degraded {
			t.Errorf("degraded=%v: got %v", degraded, rs.RAGDegraded)
		}
		if !degraded && strings.Contains(string(data), "rag_degraded") {
			t.Error("rag_degraded must be omitted when false")
		}
	}
}

func TestPrintSummary_TruncatedColumn(t *testing.T) {
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	PrintSummary([]Summary{{Model: "m", Condition: CondNoRAG, Count: 3, ErrorCount: 1, TruncatedCount: 2, AvgAccuracy: -1}})
	w.Close()
	os.Stdout = orig
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	lines := strings.Split(string(buf[:n]), "\n")
	var header, row string
	for _, l := range lines {
		if strings.HasPrefix(l, "MODEL") {
			header = l
		}
		if strings.HasPrefix(l, "m ") {
			row = l
		}
	}
	if !strings.HasSuffix(header, "TRUNCATED") {
		t.Errorf("header lacks TRUNCATED: %q", header)
	}
	if !strings.HasSuffix(strings.TrimSpace(row), "2") {
		t.Errorf("row lacks truncated count: %q", row)
	}
}

func TestComputeSummaries_TruncatedExcludesErrorRows(t *testing.T) {
	results := []Result{
		{Model: "m", Condition: CondNoRAG, Truncated: true},
		{Model: "m", Condition: CondNoRAG, Error: "boom", Truncated: true},
		{Model: "m", Condition: CondNoRAG},
	}
	s := ComputeSummaries(results)
	if len(s) != 1 || s[0].TruncatedCount != 1 {
		t.Fatalf("want TruncatedCount=1 (errors excluded), got %+v", s)
	}
}

func TestPrintTable_TruncatedColumn(t *testing.T) {
	var buf strings.Builder
	printTable(&buf, []Result{
		{Model: "mA", Condition: CondNoRAG, CaseID: "c1", Truncated: true, Accuracy: -1},
		{Model: "mB", Condition: CondNoRAG, CaseID: "c2", Accuracy: -1},
		{Model: "mC", Condition: CondNoRAG, CaseID: "c3", Error: "boom", Truncated: true, Accuracy: -1},
	})
	lines := strings.Split(buf.String(), "\n")
	var header string
	rows := map[string]string{}
	for _, l := range lines {
		if strings.HasPrefix(l, "MODEL") {
			header = l
		}
		if f := strings.Fields(l); len(f) > 0 && strings.HasPrefix(f[0], "m") && f[0] != "MODEL" {
			rows[f[0]] = l
		}
	}
	if !strings.Contains(header, "ACCURACY") || !strings.Contains(header, "TRUNC") {
		t.Fatalf("header lacks TRUNC: %q", header)
	}
	col := strings.Index(header, "TRUNC")
	if col < strings.Index(header, "ACCURACY") || col > strings.Index(header, "ERROR") {
		t.Errorf("TRUNC must sit between ACCURACY and ERROR: %q", header)
	}
	want := map[string]string{"mA": "yes", "mB": "-", "mC": "-"}
	for m, w := range want {
		row := rows[m]
		if len(row) <= col {
			t.Fatalf("%s row too short: %q", m, row)
		}
		got := strings.Fields(row[col:])[0]
		if got != w {
			t.Errorf("%s: TRUNC cell = %q, want %q (row %q)", m, got, w, row)
		}
	}
	if !strings.HasSuffix(rows["mC"], "boom") {
		t.Errorf("error row lost ERROR cell: %q", rows["mC"])
	}
}
