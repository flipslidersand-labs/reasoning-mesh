package knowledge_test

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
)

// scoreFakeQdrant mocks the retrieve (POST /points) and set_payload
// (POST /points/payload) endpoints used by ScoreUpdater.apply.
type scoreFakeQdrant struct {
	mu        sync.Mutex
	stored    map[string]map[string]any // id -> payload returned by retrieve
	getStatus int                       // 0 = 200
	getCalls  int
	updates   map[string]map[string]any // id -> last set_payload payload
	updCalls  int
	onUpdate  func() // optional hook run inside the set_payload handler
}

func (f *scoreFakeQdrant) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs     []string       `json:"ids"`
			Points  []string       `json:"points"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/points/payload"):
			f.mu.Lock()
			f.updCalls++
			if f.updates == nil {
				f.updates = map[string]map[string]any{}
			}
			f.updates[body.Points[0]] = body.Payload
			hook := f.onUpdate
			f.mu.Unlock()
			if hook != nil {
				hook()
			}
			fmt.Fprint(w, `{"status":"ok"}`)
		case strings.HasSuffix(r.URL.Path, "/points"):
			f.mu.Lock()
			f.getCalls++
			status := f.getStatus
			var res []map[string]any
			for _, id := range body.IDs {
				if p, ok := f.stored[id]; ok {
					res = append(res, map[string]any{"id": id, "payload": p})
				}
			}
			f.mu.Unlock()
			if status != 0 {
				http.Error(w, "boom", status)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": res})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
}

// runScoreApply sends one event through a real ScoreUpdater and waits for it to be
// fully processed (Stop drains the channel and joins the goroutine).
func runScoreApply(t *testing.T, f *scoreFakeQdrant, cfg *knowledge.ScorerConfig, ev knowledge.FeedbackEvent) {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	u := knowledge.NewScoreUpdater(qdrant.New(srv.URL), "rm_knowledge", 1)
	if cfg != nil {
		u.WithScorerConfig(*cfg)
	}
	u.Start()
	if !u.Send(ev) {
		t.Fatal("Send returned false")
	}
	u.Stop()
}

func payloadNum(t *testing.T, p map[string]any, key string) float64 {
	t.Helper()
	v, ok := p[key].(float64)
	if !ok {
		t.Fatalf("payload[%q] = %v (%T), want number", key, p[key], p[key])
	}
	return v
}

func TestScoreUpdater_Apply(t *testing.T) {
	type want struct{ usage, success, rate float64 }
	tests := []struct {
		name    string
		stored  map[string]map[string]any
		cfg     *knowledge.ScorerConfig
		ev      knowledge.FeedbackEvent
		getCode int
		want    map[string]want // expected updates; nil = no update at all
		getHits int
	}{
		{
			name:    "empty KnowledgeIDs returns before any RPC",
			ev:      knowledge.FeedbackEvent{Outcome: true},
			getHits: 0,
		},
		{
			name:    "batch get failure aborts without updates",
			stored:  map[string]map[string]any{"a": {"usage_count": 1.0}},
			ev:      knowledge.FeedbackEvent{KnowledgeIDs: []string{"a"}, Outcome: true},
			getCode: http.StatusInternalServerError,
			getHits: 1,
		},
		{
			name:    "ID missing from batch result is skipped, others updated",
			stored:  map[string]map[string]any{"a": {"usage_count": 1.0, "success_count": 1.0}},
			ev:      knowledge.FeedbackEvent{KnowledgeIDs: []string{"a", "ghost"}, Outcome: true},
			want:    map[string]want{"a": {2, 2, 4.0 / 6.0}},
			getHits: 1,
		},
		{
			name:    "duplicate IDs accumulate by occurrence count on success",
			stored:  map[string]map[string]any{"a": {"usage_count": 1.0, "success_count": 1.0}},
			ev:      knowledge.FeedbackEvent{KnowledgeIDs: []string{"a", "a", "a"}, Outcome: true},
			want:    map[string]want{"a": {4, 4, 6.0 / 8.0}},
			getHits: 1,
		},
		{
			name:    "failure outcome increments usage only",
			stored:  map[string]map[string]any{"a": {"usage_count": 5.0, "success_count": 2.0}},
			ev:      knowledge.FeedbackEvent{KnowledgeIDs: []string{"a", "a"}, Outcome: false},
			want:    map[string]want{"a": {7, 2, 4.0 / 11.0}},
			getHits: 1,
		},
		{
			name:    "missing counters default to zero (default prior alpha=beta=2)",
			stored:  map[string]map[string]any{"a": {}},
			ev:      knowledge.FeedbackEvent{KnowledgeIDs: []string{"a"}, Outcome: true},
			want:    map[string]want{"a": {1, 1, 3.0 / 5.0}},
			getHits: 1,
		},
		{
			name:    "WithScorerConfig changes the stored success_rate prior",
			stored:  map[string]map[string]any{"a": {}},
			cfg:     &knowledge.ScorerConfig{Alpha: 1, Beta: 3},
			ev:      knowledge.FeedbackEvent{KnowledgeIDs: []string{"a"}, Outcome: true},
			want:    map[string]want{"a": {1, 1, 2.0 / 5.0}},
			getHits: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &scoreFakeQdrant{stored: tc.stored, getStatus: tc.getCode}
			runScoreApply(t, f, tc.cfg, tc.ev)

			if f.getCalls != tc.getHits {
				t.Errorf("retrieve calls = %d, want %d", f.getCalls, tc.getHits)
			}
			if len(f.updates) != len(tc.want) {
				t.Fatalf("updated IDs = %d (%v), want %d", len(f.updates), f.updates, len(tc.want))
			}
			for id, w := range tc.want {
				p, ok := f.updates[id]
				if !ok {
					t.Fatalf("no set_payload for %q", id)
				}
				if got := payloadNum(t, p, "usage_count"); got != w.usage {
					t.Errorf("%s usage_count = %v, want %v", id, got, w.usage)
				}
				if got := payloadNum(t, p, "success_count"); got != w.success {
					t.Errorf("%s success_count = %v, want %v", id, got, w.success)
				}
				if got := payloadNum(t, p, "success_rate"); math.Abs(got-w.rate) > 1e-9 {
					t.Errorf("%s success_rate = %v, want %v", id, got, w.rate)
				}
				ts, _ := p["last_used_at"].(string)
				if _, err := time.Parse(time.RFC3339, ts); err != nil {
					t.Errorf("%s last_used_at = %q, not RFC3339: %v", id, ts, err)
				}
			}
		})
	}
}

// TestScoreUpdater_ApplyNilQdrant verifies a nil client is a no-op for a
// non-empty event (early return, no panic).
func TestScoreUpdater_ApplyNilQdrant(t *testing.T) {
	u := knowledge.NewScoreUpdater(nil, "rm_knowledge", 1)
	u.Start()
	u.Send(knowledge.FeedbackEvent{KnowledgeIDs: []string{"a"}, Outcome: true})
	u.Stop()
}

// TestScoreUpdater_ApplyConcurrencyCap verifies no more than 16 UpdatePayload
// RPCs are in flight at once when an event carries more IDs than the cap.
func TestScoreUpdater_ApplyConcurrencyCap(t *testing.T) {
	const capN, total = 16, 40
	stored := map[string]map[string]any{}
	ids := make([]string, 0, total)
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("id-%d", i)
		stored[id] = map[string]any{}
		ids = append(ids, id)
	}

	var (
		mu       sync.Mutex
		cur, max int
	)
	reached := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	f := &scoreFakeQdrant{stored: stored}
	f.onUpdate = func() {
		mu.Lock()
		cur++
		if cur > max {
			max = cur
		}
		if cur == capN {
			once.Do(func() { close(reached) })
		}
		mu.Unlock()
		<-release // hold the request until the test releases the batch
		mu.Lock()
		cur--
		mu.Unlock()
	}

	// Release once the cap is saturated: the first 16 are blocked in the
	// handler, so any further in-flight request would push max above 16
	// before it is observed after Stop below.
	go func() {
		select {
		case <-reached:
		case <-time.After(5 * time.Second):
		}
		close(release)
	}()

	runScoreApply(t, f, nil, knowledge.FeedbackEvent{KnowledgeIDs: ids, Outcome: true})

	if f.updCalls != total {
		t.Errorf("set_payload calls = %d, want %d", f.updCalls, total)
	}
	if max != capN {
		t.Errorf("max concurrent set_payload = %d, want %d", max, capN)
	}
}

// TestScoreUpdater_Dropped verifies Dropped() counts events rejected by a full buffer.
func TestScoreUpdater_Dropped(t *testing.T) {
	// Unstarted updater with buffer 1: first Send fits, the rest drop.
	u := knowledge.NewScoreUpdater(nil, "rm_knowledge", 1)
	if got := u.Dropped(); got != 0 {
		t.Fatalf("initial Dropped = %d, want 0", got)
	}
	ev := knowledge.FeedbackEvent{KnowledgeIDs: []string{"x"}}
	if !u.Send(ev) {
		t.Fatal("first Send should enqueue")
	}
	for i := 0; i < 3; i++ {
		if u.Send(ev) {
			t.Fatal("Send into a full buffer should return false")
		}
	}
	if got := u.Dropped(); got != 3 {
		t.Errorf("Dropped = %d, want 3", got)
	}
	u.Stop()
	// Send after Stop is rejected but is not a buffer-full drop.
	u.Send(ev)
	if got := u.Dropped(); got != 3 {
		t.Errorf("Dropped after Stop = %d, want 3", got)
	}
}
