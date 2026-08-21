package knowledge_test

import (
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
