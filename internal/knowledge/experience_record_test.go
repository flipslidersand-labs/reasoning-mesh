package knowledge_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
)

type expRecUpsert struct {
	Points []struct {
		ID      string         `json:"id"`
		Vector  []float32      `json:"vector"`
		Payload map[string]any `json:"payload"`
	} `json:"points"`
}

type expRecEnv struct {
	mu          sync.Mutex
	embedCalls  int
	embedTexts  []string
	upsertPaths []string
	upsertBody  expRecUpsert
	upsertCalls int
}

// expRecSetup starts mock embedding-svc and Qdrant servers.
// embedHandler/qdrantStatus customise the failure modes.
func expRecSetup(t *testing.T, embedHandler func(w http.ResponseWriter, r *http.Request), qdrantStatus int) (*expRecEnv, *knowledge.ExperienceStore) {
	t.Helper()
	env := &expRecEnv{}

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
		embedHandler(w, r)
	}))
	t.Cleanup(emb.Close)

	qd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		env.mu.Lock()
		env.upsertCalls++
		env.upsertPaths = append(env.upsertPaths, r.Method+" "+r.URL.Path)
		_ = json.Unmarshal(b, &env.upsertBody)
		env.mu.Unlock()
		w.WriteHeader(qdrantStatus)
		_, _ = w.Write([]byte(`{"status":"x"}`))
	}))
	t.Cleanup(qd.Close)

	store := knowledge.NewExperienceStore(
		knowledge.NewEmbedder(emb.URL),
		qdrant.New(qd.URL),
		"rm_experience",
	)
	return env, store
}

func expRecEmbedOK(vectors string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"vectors":` + vectors + `}`))
	}
}

func TestExperienceRecord_Success_Payload(t *testing.T) {
	env, store := expRecSetup(t, expRecEmbedOK(`[[0.5,0.25,0.125]]`), http.StatusOK)

	rec := knowledge.FailureRecord{
		Content:             "nil pointer in handler",
		TaskType:            eval.TaskDebugging,
		FailureType:         qdrant.FailureRuntime,
		Evaluator:           "ci",
		Confidence:          0.75,
		RelatedKnowledgeIDs: []string{"k1", "k2"},
	}
	before := time.Now().UTC()
	if err := store.Record(context.Background(), rec); err != nil {
		t.Fatalf("Record: %v", err)
	}
	after := time.Now().UTC()

	if !reflect.DeepEqual(env.embedTexts, []string{rec.Content}) {
		t.Errorf("embed texts = %v, want [%q]", env.embedTexts, rec.Content)
	}
	if env.upsertCalls != 1 {
		t.Fatalf("upsert calls = %d, want 1", env.upsertCalls)
	}
	if want := []string{"PUT /collections/rm_experience/points"}; !reflect.DeepEqual(env.upsertPaths, want) {
		t.Errorf("upsert paths = %v, want %v", env.upsertPaths, want)
	}
	if len(env.upsertBody.Points) != 1 {
		t.Fatalf("points = %d, want 1", len(env.upsertBody.Points))
	}
	p := env.upsertBody.Points[0]
	wantID := knowledge.ExportedExperienceID(rec.TaskType, rec.FailureType, rec.Content)
	if p.ID != wantID {
		t.Errorf("id = %q, want %q", p.ID, wantID)
	}
	if want := []float32{0.5, 0.25, 0.125}; !reflect.DeepEqual(p.Vector, want) {
		t.Errorf("vector = %v, want %v", p.Vector, want)
	}

	wantStr := map[string]string{
		"content":        rec.Content,
		"task_type":      "debugging",
		"outcome_status": "failure",
		"failure_type":   "runtime",
		"evaluator":      "ci",
	}
	for k, want := range wantStr {
		if got, _ := p.Payload[k].(string); got != want {
			t.Errorf("payload[%s] = %v, want %q", k, p.Payload[k], want)
		}
	}
	if got, _ := p.Payload["confidence"].(float64); got != 0.75 {
		t.Errorf("payload[confidence] = %v, want 0.75", p.Payload["confidence"])
	}
	rel, _ := p.Payload["related_knowledge_ids"].([]any)
	if !reflect.DeepEqual(rel, []any{"k1", "k2"}) {
		t.Errorf("payload[related_knowledge_ids] = %v, want [k1 k2]", p.Payload["related_knowledge_ids"])
	}
	cs, _ := p.Payload["created_at"].(string)
	ts, err := time.Parse(time.RFC3339, cs)
	if err != nil {
		t.Fatalf("created_at %q not RFC3339: %v", cs, err)
	}
	if ts.Before(before.Truncate(time.Second).Add(-time.Second)) || ts.After(after.Add(time.Second)) {
		t.Errorf("created_at %v outside [%v, %v]", ts, before, after)
	}
	if len(p.Payload) != 8 {
		t.Errorf("payload has %d keys, want 8: %v", len(p.Payload), p.Payload)
	}
}

func TestExperienceRecord_Idempotent_SameIDOnRepeat(t *testing.T) {
	env, store := expRecSetup(t, expRecEmbedOK(`[[1,2]]`), http.StatusOK)
	rec := knowledge.FailureRecord{Content: "c", TaskType: eval.TaskTesting, FailureType: qdrant.FailureTest}

	var ids []string
	for i := 0; i < 2; i++ {
		if err := store.Record(context.Background(), rec); err != nil {
			t.Fatalf("Record #%d: %v", i, err)
		}
		ids = append(ids, env.upsertBody.Points[0].ID)
	}
	if ids[0] != ids[1] {
		t.Errorf("repeat Record produced different IDs: %v", ids)
	}

	rec.FailureType = qdrant.FailureCompile
	if err := store.Record(context.Background(), rec); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if got := env.upsertBody.Points[0].ID; got == ids[0] {
		t.Errorf("different failure_type must change ID, got same %q", got)
	}
}

func TestExperienceRecord_Errors(t *testing.T) {
	tests := []struct {
		name           string
		taskType       eval.TaskType
		embed          func(http.ResponseWriter, *http.Request)
		qdrantStatus   int
		wantErrContain []string
		wantEmbedCalls int
		wantUpserts    int
	}{
		{
			name:           "invalid task_type",
			taskType:       eval.TaskType("bogus"),
			embed:          expRecEmbedOK(`[[1]]`),
			qdrantStatus:   http.StatusOK,
			wantErrContain: []string{"experience: invalid task_type", `"bogus"`},
		},
		{
			name:           "empty task_type",
			taskType:       eval.TaskType(""),
			embed:          expRecEmbedOK(`[[1]]`),
			qdrantStatus:   http.StatusOK,
			wantErrContain: []string{"experience: invalid task_type"},
		},
		{
			name:     "embed http 500",
			taskType: eval.TaskDebugging,
			embed: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			qdrantStatus:   http.StatusOK,
			wantErrContain: []string{"experience embed: ", "status 500"},
			wantEmbedCalls: 1,
		},
		{
			name:     "embed invalid json",
			taskType: eval.TaskDebugging,
			embed: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("not json"))
			},
			qdrantStatus:   http.StatusOK,
			wantErrContain: []string{"experience embed: "},
			wantEmbedCalls: 1,
		},
		{
			name:           "no vectors",
			taskType:       eval.TaskDebugging,
			embed:          expRecEmbedOK(`[]`),
			qdrantStatus:   http.StatusOK,
			wantErrContain: []string{"experience embed: empty vector"},
			wantEmbedCalls: 1,
		},
		{
			name:           "empty first vector",
			taskType:       eval.TaskDebugging,
			embed:          expRecEmbedOK(`[[]]`),
			qdrantStatus:   http.StatusOK,
			wantErrContain: []string{"experience embed: empty vector"},
			wantEmbedCalls: 1,
		},
		{
			name:           "qdrant upsert 500",
			taskType:       eval.TaskDebugging,
			embed:          expRecEmbedOK(`[[1,2]]`),
			qdrantStatus:   http.StatusInternalServerError,
			wantErrContain: []string{"experience upsert: ", "qdrant upsert: status 500"},
			wantEmbedCalls: 1,
			wantUpserts:    1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, store := expRecSetup(t, tt.embed, tt.qdrantStatus)
			err := store.Record(context.Background(), knowledge.FailureRecord{
				Content:     "x",
				TaskType:    tt.taskType,
				FailureType: qdrant.FailureTest,
			})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			for _, s := range tt.wantErrContain {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("error %q does not contain %q", err, s)
				}
			}
			if env.embedCalls != tt.wantEmbedCalls {
				t.Errorf("embed calls = %d, want %d", env.embedCalls, tt.wantEmbedCalls)
			}
			if env.upsertCalls != tt.wantUpserts {
				t.Errorf("upsert calls = %d, want %d", env.upsertCalls, tt.wantUpserts)
			}
		})
	}
}

func TestExperienceRecord_EmbedErrorIsWrapped(t *testing.T) {
	// A connection failure must surface through the "experience embed" wrap.
	emb := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := emb.URL
	emb.Close()
	store := knowledge.NewExperienceStore(knowledge.NewEmbedder(url), qdrant.New(url), "rm_experience")

	err := store.Record(context.Background(), knowledge.FailureRecord{Content: "x", TaskType: eval.TaskDebugging})
	if err == nil || !strings.HasPrefix(err.Error(), "experience embed: ") {
		t.Fatalf("err = %v, want prefix %q", err, "experience embed: ")
	}
}

func TestExperienceID_Distinctness(t *testing.T) {
	base := knowledge.ExportedExperienceID(eval.TaskDebugging, qdrant.FailureTest, "content")
	if len(base) != 32 {
		t.Errorf("id length = %d, want 32 hex chars", len(base))
	}
	ids := map[string]string{
		"taskType":    knowledge.ExportedExperienceID(eval.TaskTesting, qdrant.FailureTest, "content"),
		"failureType": knowledge.ExportedExperienceID(eval.TaskDebugging, qdrant.FailureCompile, "content"),
		"content":     knowledge.ExportedExperienceID(eval.TaskDebugging, qdrant.FailureTest, "content2"),
	}
	for name, id := range ids {
		if id == base {
			t.Errorf("changing %s did not change ID", name)
		}
	}
	if again := knowledge.ExportedExperienceID(eval.TaskDebugging, qdrant.FailureTest, "content"); again != base {
		t.Errorf("same input gave %q and %q", base, again)
	}
}
