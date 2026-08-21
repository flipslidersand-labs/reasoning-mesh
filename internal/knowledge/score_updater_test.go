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
