package knowledge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/ollama"
	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
)

type extRunPoint struct {
	ID      string         `json:"id"`
	Vector  []float32      `json:"vector"`
	Payload map[string]any `json:"payload"`
}

// extRunEnv records every request the Extractor.Run pipeline sends to the
// stubbed Ollama / embedder / Qdrant servers. Failures are injected per call
// number (1-based) via the *Call fields.
type extRunEnv struct {
	mu sync.Mutex

	embedCalls [][]string      // texts per /embed/batch request
	puts       [][]extRunPoint // points per PUT request (including failed ones)
	gets       int
	embedFail  map[int]bool // embed call number -> HTTP 500
	embedShort map[int]int  // embed call number -> number of vectors to drop from the tail
	embedEmpty map[int]int  // embed call number -> index whose vector is returned empty
	putFail    map[int]bool // PUT call number -> HTTP 500
	getFail    map[int]bool // GET (preserveExistingStats) call number -> HTTP 500

	ex *knowledge.Extractor
}

func newExtRunEnv(t *testing.T, batchSize int) *extRunEnv {
	t.Helper()
	env := &extRunEnv{
		embedFail:  map[int]bool{},
		embedShort: map[int]int{},
		embedEmpty: map[int]int{},
		putFail:    map[int]bool{},
		getFail:    map[int]bool{},
	}
	ol := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":    "stub",
			"response": `{"task_type":"testing","language":"go","framework":"gin","tags":["a","b"]}`,
			"done":     true,
		})
	}))
	t.Cleanup(ol.Close)

	es := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Texts []string `json:"texts"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		env.mu.Lock()
		env.embedCalls = append(env.embedCalls, req.Texts)
		n := len(env.embedCalls)
		fail := env.embedFail[n]
		drop := env.embedShort[n]
		emptyIdx, hasEmpty := env.embedEmpty[n]
		env.mu.Unlock()
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		vecs := make([][]float32, len(req.Texts))
		for i := range vecs {
			vecs[i] = extRunVec(req.Texts[i])
		}
		if hasEmpty {
			vecs[emptyIdx] = []float32{}
		}
		vecs = vecs[:len(vecs)-drop]
		_ = json.NewEncoder(w).Encode(map[string]any{"vectors": vecs})
	}))
	t.Cleanup(es.Close)

	qs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.mu.Lock()
		defer env.mu.Unlock()
		switch r.Method {
		case http.MethodPost: // GetByIDs
			env.gets++
			if env.getFail[env.gets] {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": []any{}})
		case http.MethodPut: // BulkUpsert
			var req struct {
				Points []extRunPoint `json:"points"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			env.puts = append(env.puts, req.Points)
			if env.putFail[len(env.puts)] {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"status": "acknowledged"}})
		default:
			http.Error(w, "unexpected", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(qs.Close)

	env.ex = knowledge.NewExtractorWithBatchSize(
		knowledge.NewStructurizer(ollama.New(ol.URL, 5), "m"),
		knowledge.NewEmbedder(es.URL),
		qdrant.New(qs.URL),
		"rm_knowledge",
		batchSize,
	)
	return env
}

// extRunDiff builds a diff that ChunkDiff splits into exactly n chunks
// (one per "diff --git" section, each longer than the minimum chunk length).
func extRunDiff(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "diff --git a/file%d.go b/file%d.go\n@@ -1 +1 @@\n+func unique%d() { /* body of chunk number %d */ }\n", i, i, i, i)
	}
	return b.String()
}

// extRunVec returns a vector unique to the chunk number embedded in text
// ("unique<N>("), so a mismatch between items and vectors is detectable.
func extRunVec(text string) []float32 {
	return []float32{float32(extRunNum(text)), 0.5}
}

func extRunNum(text string) int {
	var n int
	i := strings.Index(text, "unique")
	if i < 0 {
		return -1
	}
	_, _ = fmt.Sscanf(text[i:], "unique%d(", &n)
	return n
}

// extRunCheckVectors asserts each point carries the vector of its own content.
func extRunCheckVectors(t *testing.T, pts []extRunPoint) {
	t.Helper()
	for _, p := range pts {
		c, _ := p.Payload["content"].(string)
		if len(p.Vector) != 2 || int(p.Vector[0]) != extRunNum(c) {
			t.Errorf("point %q has vector %v, want vector of chunk %d", c, p.Vector, extRunNum(c))
		}
	}
}

func (e *extRunEnv) putSizes() []int {
	sizes := make([]int, len(e.puts))
	for i, p := range e.puts {
		sizes[i] = len(p)
	}
	return sizes
}

func extRunAssertSizes(t *testing.T, what string, got, want []int) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func extRunCaptureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

func TestExtractorRun_NoChunks(t *testing.T) {
	env := newExtRunEnv(t, 2)
	if err := env.ex.Run(context.Background(), "sha", "", ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(env.embedCalls) != 0 || len(env.puts) != 0 || env.gets != 0 {
		t.Errorf("expected no requests, got embed=%d put=%d get=%d", len(env.embedCalls), len(env.puts), env.gets)
	}
}

func TestExtractorRun_BatchBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		chunks    int
		batchSize int
		want      []int
	}{
		{"remainder", 5, 2, []int{2, 2, 1}},
		{"exact multiple", 4, 2, []int{2, 2}},
		{"single chunk", 1, 2, []int{1}},
		{"fewer than batch", 3, 10, []int{3}},
		{"batch of one", 3, 1, []int{1, 1, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newExtRunEnv(t, tt.batchSize)
			if err := env.ex.Run(context.Background(), "sha1", extRunDiff(tt.chunks), ""); err != nil {
				t.Fatalf("Run: %v", err)
			}
			extRunAssertSizes(t, "upsert sizes", env.putSizes(), tt.want)
			embedSizes := make([]int, len(env.embedCalls))
			for i, c := range env.embedCalls {
				embedSizes[i] = len(c)
			}
			extRunAssertSizes(t, "embed sizes", embedSizes, tt.want)
			if env.gets != len(tt.want) {
				t.Errorf("GetByIDs calls = %d, want %d", env.gets, len(tt.want))
			}
			// Every chunk is upserted exactly once, in order, with a distinct ID
			// and its own vector.
			seen := map[string]bool{}
			next := 0
			for _, pts := range env.puts {
				extRunCheckVectors(t, pts)
				for _, p := range pts {
					c, _ := p.Payload["content"].(string)
					if extRunNum(c) != next {
						t.Errorf("point order: got chunk %d, want %d", extRunNum(c), next)
					}
					next++
					if seen[p.ID] {
						t.Errorf("duplicate point ID %s", p.ID)
					}
					seen[p.ID] = true
				}
			}
			if len(seen) != tt.chunks {
				t.Errorf("unique points = %d, want %d", len(seen), tt.chunks)
			}
		})
	}
}

func TestExtractorRun_PayloadDefaults(t *testing.T) {
	env := newExtRunEnv(t, 2)
	if err := env.ex.Run(context.Background(), "abc123", extRunDiff(1), ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(env.puts) != 1 || len(env.puts[0]) != 1 {
		t.Fatalf("puts = %v, want one request with one point", env.putSizes())
	}
	p := env.puts[0][0]
	pl := p.Payload
	content, _ := pl["content"].(string)
	if want := knowledge.ExportedDeterministicID(eval.TaskTesting, content); p.ID != want {
		t.Errorf("ID = %s, want %s", p.ID, want)
	}
	if !strings.Contains(content, "unique0") {
		t.Errorf("content = %q", content)
	}
	for k, want := range map[string]any{
		"task_type": "testing", "language": "go", "framework": "gin", "source": "ci",
		"commit_sha": "abc123", "usage_count": float64(0), "success_count": float64(0), "success_rate": 0.5,
	} {
		if pl[k] != want {
			t.Errorf("payload[%s] = %v, want %v", k, pl[k], want)
		}
	}
	if tags, _ := pl["tags"].([]any); len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Errorf("tags = %v", pl["tags"])
	}
	if pl["created_at"] == "" || pl["created_at"] == nil || pl["created_at"] != pl["last_used_at"] {
		t.Errorf("created_at/last_used_at = %v / %v", pl["created_at"], pl["last_used_at"])
	}
}

func TestExtractorRun_EmbedFailureSkipsOnlyThatBatch(t *testing.T) {
	env := newExtRunEnv(t, 2)
	env.embedFail[2] = true // second batch
	logs := extRunCaptureLog(t)
	if err := env.ex.Run(context.Background(), "sha", extRunDiff(5), ""); err != nil {
		t.Fatalf("Run must not return an error on partial failure: %v", err)
	}
	if len(env.embedCalls) != 3 {
		t.Errorf("embed calls = %d, want 3 (later batches must still run)", len(env.embedCalls))
	}
	extRunAssertSizes(t, "upsert sizes", env.putSizes(), []int{2, 1})
	if !strings.Contains(logs.String(), "2/5 chunks skipped") {
		t.Errorf("expected skipped summary, log:\n%s", logs.String())
	}
}

func TestExtractorRun_AllEmbedsFail(t *testing.T) {
	env := newExtRunEnv(t, 2)
	env.embedFail[1], env.embedFail[2], env.embedFail[3] = true, true, true
	logs := extRunCaptureLog(t)
	if err := env.ex.Run(context.Background(), "sha", extRunDiff(5), ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(env.puts) != 0 || env.gets != 0 {
		t.Errorf("no lookup/upsert expected, got get=%d put=%d", env.gets, len(env.puts))
	}
	if !strings.Contains(logs.String(), "WARNING all 5 chunks were skipped") {
		t.Errorf("expected all-skipped warning, log:\n%s", logs.String())
	}
}

func TestExtractorRun_ShortAndEmptyVectorsAreSkipped(t *testing.T) {
	env := newExtRunEnv(t, 3)
	env.embedShort[1] = 1 // batch 1: 3 texts, 2 vectors -> last item skipped
	env.embedEmpty[2] = 0 // batch 2: first vector empty -> skipped
	logs := extRunCaptureLog(t)
	if err := env.ex.Run(context.Background(), "sha", extRunDiff(6), ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	extRunAssertSizes(t, "upsert sizes", env.putSizes(), []int{2, 2})
	for _, pts := range env.puts {
		extRunCheckVectors(t, pts)
	}
	// The point kept from batch 1 must be the first two texts, not the dropped third.
	for i, p := range env.puts[0] {
		if c, _ := p.Payload["content"].(string); !strings.Contains(c, fmt.Sprintf("unique%d(", i)) {
			t.Errorf("batch1 point %d content = %q", i, c)
		}
	}
	// Batch 2 drops the item whose vector was empty (unique3), keeping unique4/5.
	for i, p := range env.puts[1] {
		if c, _ := p.Payload["content"].(string); !strings.Contains(c, fmt.Sprintf("unique%d(", i+4)) {
			t.Errorf("batch2 point %d content = %q", i, c)
		}
	}
	if !strings.Contains(logs.String(), "2/6 chunks skipped") {
		t.Errorf("expected 2/6 skipped, log:\n%s", logs.String())
	}
}

func TestExtractorRun_NoVectorsReturnedNoUpsert(t *testing.T) {
	env := newExtRunEnv(t, 2)
	env.embedShort[1] = 2 // zero vectors returned for the only batch
	if err := env.ex.Run(context.Background(), "sha", extRunDiff(2), ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(env.puts) != 0 || env.gets != 0 {
		t.Errorf("no lookup/upsert expected, got get=%d put=%d", env.gets, len(env.puts))
	}
}

func TestExtractorRun_UpsertFailureContinuesWithNextBatch(t *testing.T) {
	env := newExtRunEnv(t, 2)
	env.putFail[1] = true
	logs := extRunCaptureLog(t)
	if err := env.ex.Run(context.Background(), "sha", extRunDiff(5), ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	extRunAssertSizes(t, "upsert attempts", env.putSizes(), []int{2, 2, 1})
	if !strings.Contains(logs.String(), "2/5 chunks skipped") {
		t.Errorf("expected 2/5 skipped, log:\n%s", logs.String())
	}
}

func TestExtractorRun_LookupFailureSkipsBatchWithoutUpsert(t *testing.T) {
	env := newExtRunEnv(t, 2)
	env.getFail[1] = true
	logs := extRunCaptureLog(t)
	if err := env.ex.Run(context.Background(), "sha", extRunDiff(5), ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if env.gets != 3 {
		t.Errorf("GetByIDs calls = %d, want 3", env.gets)
	}
	if !strings.Contains(logs.String(), "2/5 chunks skipped") {
		t.Errorf("expected 2/5 skipped, log:\n%s", logs.String())
	}
	// First batch must not be upserted (would reset stats); the rest continue.
	extRunAssertSizes(t, "upsert sizes", env.putSizes(), []int{2, 1})
}

func TestExtractorRun_CILogChunksAreIncluded(t *testing.T) {
	env := newExtRunEnv(t, 2)
	ciLog := "--- FAIL: TestSomething (0.00s)\n    something_test.go:10: unexpected failure in the pipeline\n\n"
	if err := env.ex.Run(context.Background(), "sha", extRunDiff(1), ciLog); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// 1 diff chunk + 1 CI chunk fit in a single batch of 2.
	extRunAssertSizes(t, "upsert sizes", env.putSizes(), []int{2})
}
