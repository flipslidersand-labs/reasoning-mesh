package knowledge_test

import (
	"context"
	"encoding/json"
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

// fakeQdrant serves GetByIDs (POST /points) and Upsert (PUT /points) in memory.
type fakeQdrant struct {
	mu       sync.Mutex
	existing map[string]map[string]any // keyed by dashed UUID, as Qdrant returns it
	anyStats map[string]any            // if set, every requested ID "exists" with this payload
	getFail  bool
	upserts  []map[string]any // payloads received via PUT
}

func (f *fakeQdrant) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodPost:
		if f.getFail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		var req struct {
			IDs []string `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var out []map[string]any
		for _, id := range req.IDs {
			d := dashed(id)
			if p, ok := f.existing[d]; ok {
				out = append(out, map[string]any{"id": d, "payload": p})
			} else if f.anyStats != nil {
				out = append(out, map[string]any{"id": d, "payload": f.anyStats})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": out})
	case http.MethodPut:
		var req struct {
			Points []struct {
				Payload map[string]any `json:"payload"`
			} `json:"points"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		for _, p := range req.Points {
			f.upserts = append(f.upserts, p.Payload)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"status": "acknowledged"}})
	default:
		http.Error(w, "unexpected", http.StatusMethodNotAllowed)
	}
}

// dashed converts 32-hex IDs to the canonical 8-4-4-4-12 UUID form Qdrant returns.
func dashed(id string) string {
	if len(id) != 32 {
		return id
	}
	return id[0:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}

func newIngestFixture(t *testing.T, fq *fakeQdrant) *knowledge.IngestStore {
	t.Helper()
	qs := httptest.NewServer(http.HandlerFunc(fq.handler))
	t.Cleanup(qs.Close)
	es := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"vectors": [][]float32{{0.1, 0.2}}})
	}))
	t.Cleanup(es.Close)
	return knowledge.NewIngestStore(knowledge.NewEmbedder(es.URL), qdrant.New(qs.URL), "rm_knowledge")
}

func TestIngest_ReingestPreservesCounters(t *testing.T) {
	content := "use errgroup for fan-out"
	id := knowledge.ExportedDeterministicID(eval.TaskImplementation, content)
	fq := &fakeQdrant{existing: map[string]map[string]any{
		dashed(id): {
			"content":       content,
			"usage_count":   7,
			"success_count": 5,
			"success_rate":  0.7,
			"created_at":    "2026-01-01T00:00:00Z",
			"last_used_at":  "2026-02-01T00:00:00Z",
		},
	}}
	store := newIngestFixture(t, fq)

	if _, err := store.Ingest(context.Background(), knowledge.IngestEntry{Content: content, Tags: []string{"new"}}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(fq.upserts) != 1 {
		t.Fatalf("upserts = %d, want 1", len(fq.upserts))
	}
	p := fq.upserts[0]
	if p["usage_count"] != float64(7) || p["success_count"] != float64(5) || p["success_rate"] != 0.7 {
		t.Errorf("counters reset: usage=%v success=%v rate=%v", p["usage_count"], p["success_count"], p["success_rate"])
	}
	if p["created_at"] != "2026-01-01T00:00:00Z" || p["last_used_at"] != "2026-02-01T00:00:00Z" {
		t.Errorf("timestamps reset: created_at=%v last_used_at=%v", p["created_at"], p["last_used_at"])
	}
	if tags, _ := p["tags"].([]any); len(tags) != 1 || tags[0] != "new" {
		t.Errorf("tags not updated: %v", p["tags"])
	}
}

func TestIngest_NewEntryStartsAtZero(t *testing.T) {
	fq := &fakeQdrant{existing: map[string]map[string]any{}}
	store := newIngestFixture(t, fq)

	if _, err := store.Ingest(context.Background(), knowledge.IngestEntry{Content: "fresh"}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(fq.upserts) != 1 {
		t.Fatalf("upserts = %d, want 1", len(fq.upserts))
	}
	p := fq.upserts[0]
	if p["usage_count"] != float64(0) || p["success_count"] != float64(0) || p["success_rate"] != 0.5 {
		t.Errorf("new entry defaults wrong: %v", p)
	}
}

func TestIngest_LookupFailureDoesNotOverwrite(t *testing.T) {
	fq := &fakeQdrant{getFail: true}
	store := newIngestFixture(t, fq)

	_, err := store.Ingest(context.Background(), knowledge.IngestEntry{Content: "x"})
	if err == nil || !strings.Contains(err.Error(), "lookup existing") {
		t.Fatalf("err = %v, want lookup existing error", err)
	}
	if len(fq.upserts) != 0 {
		t.Errorf("upserted %d points despite lookup failure", len(fq.upserts))
	}
}

const sampleDiff = `diff --git a/main.go b/main.go
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

// newExtractorFixture stubs Ollama with a fixed structurizer answer so the
// test stays focused on the upsert path.
func newExtractorFixture(t *testing.T, fq *fakeQdrant) *knowledge.Extractor {
	t.Helper()
	ol := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": `{"task_type":"implementation","language":"go","framework":"","tags":[]}`,
			"done":     true,
		})
	}))
	t.Cleanup(ol.Close)
	qs := httptest.NewServer(http.HandlerFunc(fq.handler))
	t.Cleanup(qs.Close)
	es := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Texts []string `json:"texts"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		vecs := make([][]float32, len(req.Texts))
		for i := range vecs {
			vecs[i] = []float32{0.1, 0.2}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"vectors": vecs})
	}))
	t.Cleanup(es.Close)
	s := knowledge.NewStructurizer(ollama.New(ol.URL, 5), "m")
	return knowledge.NewExtractor(s, knowledge.NewEmbedder(es.URL), qdrant.New(qs.URL), "rm_knowledge")
}

func TestExtractor_RerunPreservesCounters(t *testing.T) {
	fq := &fakeQdrant{anyStats: map[string]any{"usage_count": 3, "success_count": 2, "created_at": "2026-01-01T00:00:00Z"}}
	ex := newExtractorFixture(t, fq)

	if err := ex.Run(context.Background(), "deadbeef", sampleDiff, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fq.upserts) == 0 {
		t.Fatal("no points upserted")
	}
	for _, p := range fq.upserts {
		if p["usage_count"] != float64(3) || p["success_count"] != float64(2) || p["created_at"] != "2026-01-01T00:00:00Z" {
			t.Errorf("stats reset on re-run: %v", p)
		}
		if p["commit_sha"] != "deadbeef" {
			t.Errorf("commit_sha not refreshed: %v", p["commit_sha"])
		}
	}
}

func TestExtractor_LookupFailureSkipsBatch(t *testing.T) {
	fq := &fakeQdrant{getFail: true}
	ex := newExtractorFixture(t, fq)

	if err := ex.Run(context.Background(), "deadbeef", sampleDiff, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fq.upserts) != 0 {
		t.Errorf("upserted %d points despite lookup failure", len(fq.upserts))
	}
}
