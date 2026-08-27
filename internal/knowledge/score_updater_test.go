package knowledge_test

import (
	"sync"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
)

func TestScoreUpdater_SendAndStop(t *testing.T) {
	// ScoreUpdater with no Qdrant — just verifies channel/goroutine lifecycle.
	// GetByID will fail gracefully; we only verify no panic or deadlock.
	u := knowledge.NewScoreUpdater(nil, "rm_knowledge", 8)
	u.Start()

	var wg sync.WaitGroup
	wg.Add(3)
	for i := 0; i < 3; i++ {
		go func() {
			defer wg.Done()
			u.Send(knowledge.FeedbackEvent{
				KnowledgeIDs: []string{"abc", "def"},
				Outcome:      true,
			})
		}()
	}
	wg.Wait()
	u.Stop() // must not deadlock
}

func TestScoreUpdater_BufferFull(t *testing.T) {
	// Buffer size 0: all Sends drop without blocking.
	u := knowledge.NewScoreUpdater(nil, "rm_knowledge", 0)
	u.Start()
	for i := 0; i < 10; i++ {
		u.Send(knowledge.FeedbackEvent{KnowledgeIDs: []string{"x"}, Outcome: false})
	}
	u.Stop()
}

// TestScoreUpdater_StopWithoutStart verifies Stop() does not deadlock when
// Start() has never been called.
func TestScoreUpdater_StopWithoutStart(t *testing.T) {
	u := knowledge.NewScoreUpdater(nil, "rm_knowledge", 4)
	u.Stop() // must return immediately without blocking
}

// TestScoreUpdater_DoubleStop verifies Stop() does not panic when called twice.
func TestScoreUpdater_DoubleStop(t *testing.T) {
	u := knowledge.NewScoreUpdater(nil, "rm_knowledge", 4)
	u.Start()
	u.Stop()
	u.Stop() // must not panic
}

// TestScoreUpdater_DoubleStart verifies Start() is idempotent.
func TestScoreUpdater_DoubleStart(t *testing.T) {
	u := knowledge.NewScoreUpdater(nil, "rm_knowledge", 4)
	u.Start()
	u.Start() // second call must be a no-op
	u.Stop()
}
