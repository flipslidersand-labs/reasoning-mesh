package knowledge

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
)

// retrieveFixture は embedder と Qdrant を httptest で差し替えた Retriever 環境。
type retrieveFixture struct {
	mu           sync.Mutex
	searchBody   map[string]any
	searchCalls  int
	embedStatus  int
	embedVectors [][]float32
	searchStatus int
	hits         []map[string]any
}

func newRetrieveFixture(t *testing.T, cond eval.Condition, hits []map[string]any) (*QdrantRetriever, *retrieveFixture) {
	t.Helper()
	fx := &retrieveFixture{
		embedStatus:  http.StatusOK,
		embedVectors: [][]float32{{0.1, 0.2}},
		searchStatus: http.StatusOK,
		hits:         hits,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/embed/batch", func(w http.ResponseWriter, r *http.Request) {
		if fx.embedStatus != http.StatusOK {
			http.Error(w, "boom", fx.embedStatus)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"vectors": fx.embedVectors})
	})
	mux.HandleFunc("/collections/rm_test/points/search", func(w http.ResponseWriter, r *http.Request) {
		fx.mu.Lock()
		defer fx.mu.Unlock()
		fx.searchCalls++
		fx.searchBody = nil
		_ = json.NewDecoder(r.Body).Decode(&fx.searchBody)
		if fx.searchStatus != http.StatusOK {
			http.Error(w, "boom", fx.searchStatus)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": fx.hits})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	r := NewQdrantRetriever(qdrant.New(srv.URL), NewEmbedder(srv.URL), "rm_test", DefaultScorerConfig(), cond)
	return r, fx
}

func hit(id string, score float64, payload map[string]any) map[string]any {
	return map[string]any{"id": id, "score": score, "payload": payload}
}

func itemIDs(in []eval.KnowledgeItem) []string {
	out := make([]string, len(in))
	for i, it := range in {
		out[i] = it.ID
	}
	return out
}

func TestRetrieve_CosineKeepsSearchOrderAndScore(t *testing.T) {
	// 統計は後ろの hit ほど良いが、cosine では再順位付けされないこと。
	hits := []map[string]any{
		hit("a", 0.9, map[string]any{"content": "A", "usage_count": 50.0, "success_count": 0.0}),
		hit("b", 0.8, map[string]any{"content": "B", "usage_count": 50.0, "success_count": 50.0}),
		hit("c", 0.7, map[string]any{"content": "C"}),
	}
	r, fx := newRetrieveFixture(t, eval.CondCosine, hits)

	got, err := r.Retrieve(context.Background(), "q", "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(itemIDs(got), want) {
		t.Fatalf("ids = %v, want %v", itemIDs(got), want)
	}
	for i, wantScore := range []float64{0.9, 0.8, 0.7} {
		if got[i].Score != wantScore {
			t.Errorf("item %d score = %v, want %v", i, got[i].Score, wantScore)
		}
	}
	if got[0].Content != "A" {
		t.Errorf("content = %q, want A", got[0].Content)
	}
	if lim := fx.searchBody["limit"]; lim != float64(3) {
		t.Errorf("limit = %v, want 3 (no overfetch for cosine)", lim)
	}
	if _, ok := fx.searchBody["filter"]; ok {
		t.Errorf("filter present for empty task type: %v", fx.searchBody["filter"])
	}
}

func TestRetrieve_ScoreReranksAndTrimsToTopK(t *testing.T) {
	fresh := time.Now().Add(-time.Hour).Format(time.RFC3339)
	mk := func(content string, usage, success float64) map[string]any {
		return map[string]any{
			"content": content, "usage_count": usage, "success_count": success,
			"last_used_at": fresh, "task_type": "t",
		}
	}
	// cosine 降順は a,b,c,d,e,f。成功実績により最良は f、次点は e になる。
	// c は e より cosine が高く成功実績も同じだが task_type が不一致。
	// taskType が computeScore に渡る(boost が掛かる)場合のみ e が c を上回る。
	hits := []map[string]any{
		hit("a", 0.90, mk("A", 20, 0)),
		hit("b", 0.88, mk("B", 20, 0)),
		hit("c", 0.86, mk("C", 20, 0)),
		hit("d", 0.84, mk("D", 20, 0)),
		hit("e", 0.82, mk("E", 20, 20)),
		hit("f", 0.80, mk("F", 20, 20)),
	}
	hits[2]["payload"] = map[string]any{
		"content": "C", "usage_count": 20.0, "success_count": 20.0,
		"last_used_at": fresh, "task_type": "other",
	}
	hits[5]["score"] = 0.95 // f が最高 cosine かつ高成功率 -> 先頭
	r, fx := newRetrieveFixture(t, eval.CondScore, hits)

	got, err := r.Retrieve(context.Background(), "q", "t", 2)
	if err != nil {
		t.Fatal(err)
	}
	if lim := fx.searchBody["limit"]; lim != float64(6) {
		t.Errorf("limit = %v, want 6 (topK*3)", lim)
	}
	if want := []string{"f", "e"}; !reflect.DeepEqual(itemIDs(got), want) {
		t.Fatalf("ids = %v, want %v", itemIDs(got), want)
	}
	if got[0].Score < got[1].Score {
		t.Errorf("not descending: %v < %v", got[0].Score, got[1].Score)
	}
	// 再計算値であり素の cosine ではないこと。
	if got[1].Score == 0.82 {
		t.Errorf("score not recomputed: %v", got[1].Score)
	}
}

func TestRetrieve_ScoreFewerHitsThanTopKNotPadded(t *testing.T) {
	r, _ := newRetrieveFixture(t, eval.CondScore, []map[string]any{hit("a", 0.5, map[string]any{"content": "A"})})
	got, err := r.Retrieve(context.Background(), "q", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
}

func TestRetrieve_OtherConditionNoOverfetchNoRerank(t *testing.T) {
	hits := []map[string]any{
		hit("a", 0.9, map[string]any{"content": "A"}),
		hit("b", 0.8, map[string]any{"content": "B"}),
	}
	r, fx := newRetrieveFixture(t, eval.CondCompressed, hits)
	got, err := r.Retrieve(context.Background(), "q", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if lim := fx.searchBody["limit"]; lim != float64(2) {
		t.Errorf("limit = %v, want 2", lim)
	}
	if want := []string{"a", "b"}; !reflect.DeepEqual(itemIDs(got), want) {
		t.Errorf("ids = %v, want %v", itemIDs(got), want)
	}
}

func TestRetrieve_CosineAndOtherConditionsDoNotRerank(t *testing.T) {
	// Beta スコアなら b > a となるが、cosine 条件では検索順 a,b のままであること。
	// (cosine は hit.Score をそのまま返すため、並べ替え自体が起きないことを確認する。)
	hits := []map[string]any{
		hit("a", 0.9, map[string]any{"usage_count": 20.0, "success_count": 0.0}),
		hit("b", 0.8, map[string]any{"usage_count": 20.0, "success_count": 20.0}),
	}
	for _, cond := range []eval.Condition{eval.CondCosine, eval.CondCompressed} {
		t.Run(string(cond), func(t *testing.T) {
			r, _ := newRetrieveFixture(t, cond, hits)
			got, err := r.Retrieve(context.Background(), "q", "", 2)
			if err != nil {
				t.Fatal(err)
			}
			if want := []string{"a", "b"}; !reflect.DeepEqual(itemIDs(got), want) {
				t.Errorf("ids = %v, want %v", itemIDs(got), want)
			}
		})
	}
	// 前提の確認: score 条件なら b が先頭になる入力であること。
	r, _ := newRetrieveFixture(t, eval.CondScore, hits)
	got, err := r.Retrieve(context.Background(), "q", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"b", "a"}; !reflect.DeepEqual(itemIDs(got), want) {
		t.Fatalf("precondition: score ids = %v, want %v", itemIDs(got), want)
	}
}

func TestRetrieve_TaskTypeFilterSent(t *testing.T) {
	r, fx := newRetrieveFixture(t, eval.CondCosine, nil)
	if _, err := r.Retrieve(context.Background(), "q", eval.TaskType("code_review"), 4); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"must": []any{
			map[string]any{"key": "task_type", "match": map[string]any{"value": "code_review"}},
		},
	}
	if !reflect.DeepEqual(fx.searchBody["filter"], want) {
		t.Errorf("filter = %#v, want %#v", fx.searchBody["filter"], want)
	}
	if vec, ok := fx.searchBody["vector"].([]any); !ok || len(vec) != 2 {
		t.Errorf("vector = %v, want embedder output", fx.searchBody["vector"])
	}
}

func TestRetrieve_MissingContentIsEmpty(t *testing.T) {
	hits := []map[string]any{
		hit("a", 0.9, map[string]any{}),
		hit("b", 0.8, map[string]any{"content": 123.0}),
		{"id": "c", "score": 0.7}, // payload 自体が無い
	}
	r, _ := newRetrieveFixture(t, eval.CondCosine, hits)
	got, err := r.Retrieve(context.Background(), "q", "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for _, it := range got {
		if it.Content != "" {
			t.Errorf("%s content = %q, want empty", it.ID, it.Content)
		}
	}
}

func TestRetrieve_DependencyErrors(t *testing.T) {
	t.Run("embed status error", func(t *testing.T) {
		r, fx := newRetrieveFixture(t, eval.CondCosine, nil)
		fx.embedStatus = http.StatusInternalServerError
		_, err := r.Retrieve(context.Background(), "q", "", 3)
		if err == nil || !strings.Contains(err.Error(), "embed query") {
			t.Fatalf("err = %v, want embed query wrap", err)
		}
		if fx.searchCalls != 0 {
			t.Errorf("search called %d times after embed failure", fx.searchCalls)
		}
	})
	t.Run("embed empty vectors", func(t *testing.T) {
		r2, fx2 := newRetrieveFixture(t, eval.CondCosine, nil)
		fx2.embedVectors = [][]float32{}
		_, err := r2.Retrieve(context.Background(), "q", "", 3)
		if err == nil || !strings.Contains(err.Error(), "embed query") {
			t.Fatalf("err = %v, want embed query wrap", err)
		}
	})
	t.Run("search status error", func(t *testing.T) {
		r, fx := newRetrieveFixture(t, eval.CondCosine, nil)
		fx.searchStatus = http.StatusServiceUnavailable
		_, err := r.Retrieve(context.Background(), "q", "", 3)
		if err == nil || !strings.Contains(err.Error(), "qdrant search:") || !strings.Contains(err.Error(), "503") {
			t.Fatalf("err = %v, want qdrant search error with status 503", err)
		}
	})
	t.Run("canceled context", func(t *testing.T) {
		r, _ := newRetrieveFixture(t, eval.CondCosine, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := r.Retrieve(ctx, "q", "", 3); err == nil {
			t.Fatal("want error")
		}
	})
}

func TestRetrieve_ScoreMissingStatsFinite(t *testing.T) {
	hits := []map[string]any{
		hit("a", 0.9, map[string]any{"content": "A"}),                                // usage_count 等すべて欠落
		hit("b", 0.8, map[string]any{"usage_count": "x", "last_used_at": "garbage"}), // 型不正
		hit("c", 0.7, nil),
	}
	r, _ := newRetrieveFixture(t, eval.CondScore, hits)
	got, err := r.Retrieve(context.Background(), "q", "t", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for _, it := range got {
		if math.IsNaN(it.Score) || math.IsInf(it.Score, 0) || it.Score <= 0 {
			t.Errorf("%s score = %v, want finite > 0", it.ID, it.Score)
		}
	}
}

func TestComputeScore_CosineReturnsHitScore(t *testing.T) {
	r := &QdrantRetriever{scorer: DefaultScorerConfig(), condition: eval.CondCosine}
	h := qdrant.SearchResult{Score: 0.42, Payload: map[string]any{"usage_count": 99.0, "success_count": 0.0}}
	if got := r.computeScore(h, "x"); got != 0.42 {
		t.Errorf("score = %v, want 0.42", got)
	}
}

func TestComputeScore_ScoreBranchValues(t *testing.T) {
	cfg := DefaultScorerConfig()
	r := &QdrantRetriever{scorer: cfg, condition: eval.CondScore}
	payload := map[string]any{"usage_count": 4.0, "success_count": 3.0, "task_type": "x"}

	// タイムスタンプ無し -> days=0。task_type 一致で TaskBoost が掛かる。
	match := r.computeScore(qdrant.SearchResult{Score: 0.8, Payload: payload}, "x")
	if want := cfg.Score(0.8, 4, 3, 0, true); math.Abs(match-want) > 1e-12 {
		t.Errorf("match = %v, want %v", match, want)
	}
	miss := r.computeScore(qdrant.SearchResult{Score: 0.8, Payload: payload}, "y")
	if want := cfg.Score(0.8, 4, 3, 0, false); math.Abs(miss-want) > 1e-12 {
		t.Errorf("miss = %v, want %v", miss, want)
	}
	if match <= miss {
		t.Errorf("task boost not applied: %v <= %v", match, miss)
	}

	// last_used_at が created_at より優先され、古いほど減衰する。
	old := time.Now().Add(-100 * 24 * time.Hour).Format(time.RFC3339)
	recent := time.Now().Add(-time.Hour).Format(time.RFC3339)
	withOld := map[string]any{"usage_count": 4.0, "success_count": 3.0, "last_used_at": old, "created_at": recent}
	withRecent := map[string]any{"usage_count": 4.0, "success_count": 3.0, "last_used_at": recent, "created_at": old}
	so := r.computeScore(qdrant.SearchResult{Score: 0.8, Payload: withOld}, "")
	sr := r.computeScore(qdrant.SearchResult{Score: 0.8, Payload: withRecent}, "")
	if so >= sr {
		t.Errorf("last_used_at should take precedence: old=%v recent=%v", so, sr)
	}
	// 経過日数は「日」単位: 比 so/sr = exp(-λ*(100 - 1/24))(時間単位だと大きくずれる)。
	wantRatio := math.Exp(-cfg.FreshnessLambda * (100 - 1.0/24))
	if gotRatio := so / sr; math.Abs(gotRatio-wantRatio) > 1e-6 {
		t.Errorf("so/sr = %v, want %v", gotRatio, wantRatio)
	}
	// 不正な last_used_at は created_at にフォールバックする。
	fb := map[string]any{"usage_count": 4.0, "success_count": 3.0, "last_used_at": "bad", "created_at": old}
	sf := r.computeScore(qdrant.SearchResult{Score: 0.8, Payload: fb}, "")
	if sf >= sr {
		t.Errorf("fallback to created_at not applied: %v >= %v", sf, sr)
	}
}

func TestPayloadHelpers(t *testing.T) {
	p := map[string]any{"n": 3.7, "s": "v", "bad": "str"}
	if got := payloadInt(p, "n"); got != 3 {
		t.Errorf("payloadInt = %d, want 3", got)
	}
	if got := payloadInt(p, "bad"); got != 0 {
		t.Errorf("payloadInt wrong type = %d, want 0", got)
	}
	if got := payloadInt(nil, "n"); got != 0 {
		t.Errorf("payloadInt nil map = %d, want 0", got)
	}
	if got := payloadStr(p, "s"); got != "v" {
		t.Errorf("payloadStr = %q", got)
	}
	if got := payloadStr(p, "n"); got != "" {
		t.Errorf("payloadStr wrong type = %q", got)
	}
}
