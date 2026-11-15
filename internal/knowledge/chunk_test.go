package knowledge_test

import (
	"strings"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
)

func TestChunkDiff_Empty(t *testing.T) {
	chunks := knowledge.ChunkDiff("")
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks, got %d", len(chunks))
	}
}

func TestChunkDiff_SingleHunk(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
index abc..def 100644
--- a/main.go
+++ b/main.go
@@ -10,6 +10,10 @@ func main() {
+	if err != nil {
+		return fmt.Errorf("wrap: %w", err)
+	}
 	return nil
 }
`
	chunks := knowledge.ChunkDiff(diff)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	for _, c := range chunks {
		if len(c.Content) < 10 {
			t.Errorf("chunk too short: %q", c.Content)
		}
	}
}

func TestChunkDiff_MultipleFiles(t *testing.T) {
	diff := `diff --git a/server.go b/server.go
index 111..222 100644
--- a/server.go
+++ b/server.go
@@ -5,3 +5,8 @@ package main
+// middleware added for rate limiting
+func rateLimit(next http.Handler) http.Handler {
+	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		next.ServeHTTP(w, r)
+	})
+}
diff --git a/server_test.go b/server_test.go
index 333..444 100644
--- a/server_test.go
+++ b/server_test.go
@@ -1,3 +1,10 @@ package main
+func TestRateLimit(t *testing.T) {
+	// test implementation
+	handler := rateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
+	if handler == nil {
+		t.Fatal("nil handler")
+	}
+}
`
	chunks := knowledge.ChunkDiff(diff)
	// server.go → implementation, server_test.go → testing
	if len(chunks) < 2 {
		t.Fatalf("expected ≥2 chunks, got %d", len(chunks))
	}
	taskTypes := map[eval.TaskType]bool{}
	for _, c := range chunks {
		taskTypes[c.HintTaskType] = true
	}
	if !taskTypes[eval.TaskTesting] {
		t.Errorf("expected a testing chunk (from *_test.go)")
	}
}

func TestChunkCILog_Empty(t *testing.T) {
	chunks := knowledge.ChunkCILog("")
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks, got %d", len(chunks))
	}
}

func TestChunkCILog_ErrorBlock(t *testing.T) {
	ciLog := `=== RUN   TestFoo
--- FAIL: TestFoo (0.01s)
    main_test.go:42: expected 1 got 0
    error: assertion failed

=== RUN   TestBar
--- PASS: TestBar (0.00s)
`
	chunks := knowledge.ChunkCILog(ciLog)
	if len(chunks) == 0 {
		t.Fatal("expected ≥1 chunk from error block")
	}
	if chunks[0].HintTaskType != eval.TaskDebugging {
		t.Errorf("expected debugging, got %s", chunks[0].HintTaskType)
	}
}

// hunkLine returns a single "@@" line (no newline) whose Content contribution,
// including the "\n" ChunkDiff appends, is exactly n bytes.
func hunkLine(n int) string {
	return "@@" + strings.Repeat("h", n-3)
}

type wantChunk struct {
	content string
	hint    eval.TaskType
}

func assertChunks(t *testing.T, got []knowledge.Chunk, want []wantChunk) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("chunk count: got %d, want %d (%q)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Content != w.content {
			t.Errorf("chunk %d content: got %q, want %q", i, got[i].Content, w.content)
		}
		if got[i].HintTaskType != w.hint {
			t.Errorf("chunk %d hint: got %s, want %s", i, got[i].HintTaskType, w.hint)
		}
	}
}

func TestChunkDiff_Boundaries(t *testing.T) {
	const hdr = "diff --git a/x.go b/x.go\n"
	impl := eval.TaskImplementation
	tests := []struct {
		name string
		in   string
		want []wantChunk
	}{
		{
			name: "final chunk of exactly minChunkLen(50) bytes is dropped",
			in:   hdr + hunkLine(50),
			want: nil,
		},
		{
			name: "final chunk of 51 bytes is kept",
			in:   hdr + hunkLine(51),
			want: []wantChunk{{hunkLine(51) + "\n", impl}},
		},
		{
			name: "hunk of exactly 50 bytes is not flushed at next @@ (merged)",
			in:   hdr + hunkLine(50) + "\n" + hunkLine(60),
			want: []wantChunk{{hunkLine(50) + "\n" + hunkLine(60) + "\n", impl}},
		},
		{
			name: "hunk of 51 bytes is flushed at next @@",
			in:   hdr + hunkLine(51) + "\n" + hunkLine(60),
			want: []wantChunk{
				{hunkLine(51) + "\n", impl},
				{hunkLine(60) + "\n", impl},
			},
		},
		{
			name: "small hunks are merged until the total exceeds minChunkLen",
			in:   hdr + "@@ a\n+x\n@@ b\n+" + strings.Repeat("y", 60),
			want: []wantChunk{{"@@ a\n+x\n@@ b\n+" + strings.Repeat("y", 60) + "\n", impl}},
		},
		{
			name: "small hunks totalling <= minChunkLen are dropped at EOF",
			in:   hdr + "@@ a\n+x\n@@ b\n+y",
			want: nil,
		},
		{
			// Current behaviour: diff --git flushes any non-empty buffer
			// regardless of size (no minChunkLen check). Pinned as-is.
			name: "small chunk before next diff --git is still emitted",
			in:   hdr + "@@ a\n+x\ndiff --git a/y.go b/y.go\n" + hunkLine(60),
			want: []wantChunk{
				{"@@ a\n+x\n", impl},
				{hunkLine(60) + "\n", impl},
			},
		},
		{
			name: "hunk before any diff --git uses initial debugging hint",
			in:   hunkLine(60),
			want: []wantChunk{{hunkLine(60) + "\n", eval.TaskDebugging}},
		},
		{
			name: "@@ before diff --git: first chunk debugging, next by path",
			in: hunkLine(60) + "\ndiff --git a/foo_test.go b/foo_test.go\n" +
				hunkLine(60),
			want: []wantChunk{
				{hunkLine(60) + "\n", eval.TaskDebugging},
				{hunkLine(60) + "\n", eval.TaskTesting},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertChunks(t, knowledge.ChunkDiff(tt.in), tt.want)
		})
	}
}

// TestChunkDiff_PathInference exercises inferTaskTypeFromPath through
// ChunkDiff. Cases marked "current spec" are false positives of the substring
// match; they are pinned as the current spec. Whether to fix them is a
// separate Issue (not decided here).
func TestChunkDiff_PathInference(t *testing.T) {
	tests := []struct {
		path string
		want eval.TaskType
	}{
		{"main.go", eval.TaskImplementation},
		{"foo_test.go", eval.TaskTesting},
		{"latest.go", eval.TaskTesting},  // current spec: "latest" contains "test"
		{"contest.go", eval.TaskTesting}, // current spec: "contest" contains "test"
		{"architecture.go", eval.TaskArchitecture},
		{"fix.go", eval.TaskDebugging},
		{"FIX.go", eval.TaskDebugging}, // case-insensitive
		// priority: test > arch/design > error/fix
		{"arch_test.go", eval.TaskTesting},
		{"fix_test.go", eval.TaskTesting},
		{"arch_fix.go", eval.TaskArchitecture},
		{"design_error.go", eval.TaskArchitecture},
		{"fix_design.go", eval.TaskArchitecture},
		{"error_fix.go", eval.TaskDebugging},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			in := "diff --git a/" + tt.path + " b/" + tt.path + "\n" + hunkLine(60)
			assertChunks(t, knowledge.ChunkDiff(in), []wantChunk{{hunkLine(60) + "\n", tt.want}})
		})
	}
}

func TestChunkCILog_Table(t *testing.T) {
	dbg := eval.TaskDebugging
	pad := strings.Repeat("x", 60)
	errLine := "error: " + pad
	tests := []struct {
		name string
		in   string
		want []wantChunk
	}{
		{"no error lines returns nil", "ok\nPASS\nall good\n", nil},
		{
			name: "split on blank line",
			in:   errLine + "\n\nfatal: " + pad + "\n\n",
			want: []wantChunk{
				{errLine + "\n\n", dbg},
				{"fatal: " + pad + "\n\n", dbg},
			},
		},
		{
			name: "split on --- separator",
			in:   errLine + "\n---\npanic: " + pad + "\n---\n",
			want: []wantChunk{
				{errLine + "\n---\n", dbg},
				{"panic: " + pad + "\n---\n", dbg},
			},
		},
		{
			name: "unterminated final block over minChunkLen is kept",
			in:   errLine,
			want: []wantChunk{{errLine + "\n", dbg}},
		},
		{
			name: "unterminated final block <= minChunkLen is dropped",
			in:   errLine + "\n\nerror: short",
			want: []wantChunk{{errLine + "\n\n", dbg}},
		},
		{
			name: "only a short unterminated block returns nil",
			in:   "error: short",
			want: nil,
		},
		{
			// A blank line only ends the block when the buffer exceeds
			// minChunkLen; otherwise the block keeps growing.
			name: "short block is not split at blank line",
			in:   "error: a\n\nmore " + pad,
			want: []wantChunk{{"error: a\n\nmore " + pad + "\n", dbg}},
		},
	}
	for _, kw := range []string{"ERROR", "Fail", "PaNiC", "FATAL", "error", "fail", "panic", "fatal"} {
		line := kw + " " + pad
		tests = append(tests, struct {
			name string
			in   string
			want []wantChunk
		}{"keyword " + kw, "ok\n" + line, []wantChunk{{line + "\n", dbg}}})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertChunks(t, knowledge.ChunkCILog(tt.in), tt.want)
		})
	}
}
