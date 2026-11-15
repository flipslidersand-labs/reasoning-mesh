package knowledge_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
)

func TestIngestEntry_DefaultTaskType(t *testing.T) {
	// Empty task_type should default to implementation in Ingest(),
	// so IDs should differ from a debugging entry with the same content.
	content := "some code pattern"
	idImpl := knowledge.ExportedDeterministicID(eval.TaskImplementation, content)
	idDebug := knowledge.ExportedDeterministicID(eval.TaskDebugging, content)

	if idImpl == idDebug {
		t.Error("different task types should produce different IDs (dedup must be task-scoped)")
	}
}

// #212: an unknown task_type used to be silently rewritten to "implementation"
// by toQdrantTaskType; it must now be rejected before anything is embedded/stored.
func TestIngest_UnknownTaskType_Rejected(t *testing.T) {
	s := knowledge.NewIngestStore(nil, nil, "col") // nil embedder: validation must fire first
	_, err := s.Ingest(context.Background(), knowledge.IngestEntry{Content: "x", TaskType: "bogus"})
	if err == nil || !strings.Contains(err.Error(), "task_type") {
		t.Fatalf("err = %v, want task_type validation error", err)
	}
}

// eval.TaskType and qdrant.TaskType are one type; no conversion function exists.
func TestTaskType_SharedBetweenEvalAndQdrant(t *testing.T) {
	var q qdrant.TaskType = eval.TaskDebugging
	if string(q) != "debugging" {
		t.Fatalf("got %q", q)
	}
}

func TestExperienceRecord_UnknownTaskType_Rejected(t *testing.T) {
	s := knowledge.NewExperienceStore(nil, nil, "col")
	err := s.Record(context.Background(), knowledge.FailureRecord{Content: "x", TaskType: "bogus"})
	if err == nil || !strings.Contains(err.Error(), "task_type") {
		t.Fatalf("err = %v, want task_type validation error", err)
	}
}

type ingestEnv struct {
	mu          sync.Mutex
	embedCalls  int
	embedTexts  []string
	getCalls    int
	upsertCalls int
	upsertBody  struct {
		Points []struct {
			ID      string         `json:"id"`
			Payload map[string]any `json:"payload"`
		} `json:"points"`
	}
}

type ingestCfg struct {
	embedStatus int
	embedBody   string
	getStatus   int
	getBody     string
	upsertCode  int
}

// ingestSetup stubs the embedding service and Qdrant with httptest servers.
// GetByIDs is POST and Upsert is PUT on the same path.
func ingestSetup(t *testing.T, cfg ingestCfg) (*ingestEnv, *knowledge.IngestStore) {
	t.Helper()
	env := &ingestEnv{}
	emb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Texts []string `json:"texts"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		env.mu.Lock()
		env.embedCalls++
		env.embedTexts = append(env.embedTexts, req.Texts...)
		env.mu.Unlock()
		if cfg.embedStatus != 0 {
			w.WriteHeader(cfg.embedStatus)
		}
		_, _ = w.Write([]byte(cfg.embedBody))
	}))
	t.Cleanup(emb.Close)

	qd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		env.mu.Lock()
		defer env.mu.Unlock()
		if r.Method == http.MethodPut {
			env.upsertCalls++
			_ = json.Unmarshal(b, &env.upsertBody)
			w.WriteHeader(cfg.upsertCode)
			_, _ = w.Write([]byte(`{"status":"x"}`))
			return
		}
		env.getCalls++
		w.WriteHeader(cfg.getStatus)
		_, _ = w.Write([]byte(cfg.getBody))
	}))
	t.Cleanup(qd.Close)

	return env, knowledge.NewIngestStore(knowledge.NewEmbedder(emb.URL), qdrant.New(qd.URL), "rm_knowledge")
}

const ingestEmbedOK = `{"vectors":[[0.5,0.25,0.125]]}`

func ingestOKCfg() ingestCfg {
	return ingestCfg{embedBody: ingestEmbedOK, getStatus: 200, getBody: `{"result":[]}`, upsertCode: 200}
}

func TestIngest_Content(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantErr    string
		wantUpsert int
	}{
		{"empty rejected", "", "content must not be empty", 0},
		// Current spec (pinned, not endorsed): only the exact empty string is
		// rejected; whitespace-only content passes through, is embedded and stored.
		{"whitespace-only passes through", "   \n\t", "", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, s := ingestSetup(t, ingestOKCfg())
			_, err := s.Ingest(context.Background(), knowledge.IngestEntry{Content: tc.content})
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			if env.upsertCalls != tc.wantUpsert {
				t.Errorf("upsertCalls = %d, want %d", env.upsertCalls, tc.wantUpsert)
			}
			if tc.wantErr != "" && env.embedCalls != 0 {
				t.Errorf("embed called %d times on rejected input", env.embedCalls)
			}
		})
	}
}

func TestIngest_EmptyTaskType_DefaultsToImplementation(t *testing.T) {
	env, s := ingestSetup(t, ingestOKCfg())
	id, err := s.Ingest(context.Background(), knowledge.IngestEntry{Content: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if got := env.upsertBody.Points[0].Payload["task_type"]; got != "implementation" {
		t.Errorf("task_type = %v, want implementation", got)
	}
	if want := knowledge.ExportedDeterministicID(eval.TaskImplementation, "c"); id != want {
		t.Errorf("id = %s, want %s", id, want)
	}
}

func TestIngest_EmbedFailures(t *testing.T) {
	tests := []struct {
		name string
		cfg  ingestCfg
	}{
		{"embed 500", ingestCfg{embedStatus: 500, embedBody: `{"error":"boom"}`}},
		{"empty vectors", ingestCfg{embedBody: `{"vectors":[]}`}},
		{"empty vector", ingestCfg{embedBody: `{"vectors":[[]]}`}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.getStatus, tc.cfg.getBody, tc.cfg.upsertCode = 200, `{"result":[]}`, 200
			env, s := ingestSetup(t, tc.cfg)
			_, err := s.Ingest(context.Background(), knowledge.IngestEntry{Content: "c"})
			if err == nil || !strings.HasPrefix(err.Error(), "embed:") {
				t.Fatalf("err = %v, want embed: prefix", err)
			}
			if tc.name != "embed 500" && !strings.Contains(err.Error(), "embed: empty vector") {
				t.Errorf("err = %v, want 'embed: empty vector'", err)
			}
			if env.getCalls != 0 || env.upsertCalls != 0 {
				t.Errorf("qdrant touched after embed failure: get=%d upsert=%d", env.getCalls, env.upsertCalls)
			}
		})
	}
}

func TestIngest_PreservesExistingStats(t *testing.T) {
	id := knowledge.ExportedDeterministicID(eval.TaskDebugging, "c")
	cfg := ingestOKCfg()
	cfg.getBody = `{"result":[{"id":"` + id + `","payload":{"usage_count":7,"success_count":5,"success_rate":0.7,"created_at":"2020-01-01T00:00:00Z","last_used_at":"2021-01-01T00:00:00Z"}}]}`
	env, s := ingestSetup(t, cfg)
	if _, err := s.Ingest(context.Background(), knowledge.IngestEntry{Content: "c", TaskType: eval.TaskDebugging}); err != nil {
		t.Fatal(err)
	}
	p := env.upsertBody.Points[0].Payload
	for k, want := range map[string]any{"usage_count": 7.0, "success_count": 5.0, "success_rate": 0.7, "created_at": "2020-01-01T00:00:00Z", "last_used_at": "2021-01-01T00:00:00Z"} {
		if p[k] != want {
			t.Errorf("%s = %v, want %v", k, p[k], want)
		}
	}
}

func TestIngest_PreserveLookupFailure_SkipsUpsert(t *testing.T) {
	cfg := ingestOKCfg()
	cfg.getStatus, cfg.getBody = 500, `oops`
	env, s := ingestSetup(t, cfg)
	_, err := s.Ingest(context.Background(), knowledge.IngestEntry{Content: "c"})
	if err == nil || !strings.Contains(err.Error(), "lookup existing points") {
		t.Fatalf("err = %v, want lookup existing points", err)
	}
	if env.upsertCalls != 0 {
		t.Errorf("upsertCalls = %d, want 0", env.upsertCalls)
	}
}

func TestIngest_UpsertFailure_Wrapped(t *testing.T) {
	cfg := ingestOKCfg()
	cfg.upsertCode = 500
	_, s := ingestSetup(t, cfg)
	_, err := s.Ingest(context.Background(), knowledge.IngestEntry{Content: "c"})
	if err == nil || !strings.HasPrefix(err.Error(), "upsert: ") || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("err = %v, want upsert: wrapped status 500", err)
	}
}

func TestIngest_Success_PayloadAndID(t *testing.T) {
	env, s := ingestSetup(t, ingestOKCfg())
	before := time.Now().UTC().Add(-time.Second)
	entry := knowledge.IngestEntry{
		Content: "goroutine leak: missing cancel", TaskType: eval.TaskDebugging,
		Language: "go", Framework: "gin", Tags: []string{"a", "b"},
	}
	id, err := s.Ingest(context.Background(), entry)
	if err != nil {
		t.Fatal(err)
	}
	// Ingest path and Extractor share deterministicID.
	if want := knowledge.ExportedDeterministicID(eval.TaskDebugging, entry.Content); id != want {
		t.Errorf("id = %s, want %s", id, want)
	}
	if len(env.upsertBody.Points) != 1 || env.upsertBody.Points[0].ID != id {
		t.Fatalf("upserted points = %+v, want single point with id %s", env.upsertBody.Points, id)
	}
	if len(env.embedTexts) != 1 || env.embedTexts[0] != entry.Content {
		t.Errorf("embed texts = %v", env.embedTexts)
	}
	p := env.upsertBody.Points[0].Payload
	want := map[string]any{
		"content": entry.Content, "task_type": "debugging", "language": "go", "framework": "gin",
		"source": "manual", "usage_count": 0.0, "success_count": 0.0, "success_rate": 0.5,
	}
	for k, v := range want {
		if p[k] != v {
			t.Errorf("%s = %v, want %v", k, p[k], v)
		}
	}
	tags, _ := p["tags"].([]any)
	if len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Errorf("tags = %v", p["tags"])
	}
	for _, k := range []string{"created_at", "last_used_at"} {
		ts, _ := p[k].(string)
		got, err := time.Parse(time.RFC3339, ts)
		if err != nil || got.Before(before) || got.After(time.Now().Add(time.Second)) {
			t.Errorf("%s = %q (err %v), want recent RFC3339", k, ts, err)
		}
	}
}
