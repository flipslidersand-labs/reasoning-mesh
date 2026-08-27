package knowledge

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/qdrant"
)

// FeedbackEvent is emitted when an inference result is judged.
type FeedbackEvent struct {
	KnowledgeIDs []string // IDs that were used during inference
	Outcome      bool     // true = success, false = failure
	Evaluator    string   // source label, e.g. "ci", "eval"
}

// ScoreUpdater listens on a channel and asynchronously updates Qdrant payloads.
// It increments usage_count for all referenced IDs, and success_count only on success.
type ScoreUpdater struct {
	qdrant     *qdrant.Client
	collection string
	ch         chan FeedbackEvent
	done       chan struct{}
	stopOnce   sync.Once
	started    atomic.Bool
}

// NewScoreUpdater creates a ScoreUpdater. Call Start() to begin processing.
func NewScoreUpdater(qc *qdrant.Client, collection string, bufSize int) *ScoreUpdater {
	return &ScoreUpdater{
		qdrant:     qc,
		collection: collection,
		ch:         make(chan FeedbackEvent, bufSize),
		done:       make(chan struct{}),
	}
}

// Start launches the background goroutine. Call Stop() to shut down cleanly.
// Calling Start() more than once has no additional effect.
func (u *ScoreUpdater) Start() {
	if u.started.CompareAndSwap(false, true) {
		go u.loop()
	}
}

// Stop signals the background goroutine to drain remaining events and exit.
// It is safe to call Stop() multiple times or without a prior Start().
func (u *ScoreUpdater) Stop() {
	u.stopOnce.Do(func() { close(u.ch) })
	if u.started.Load() {
		<-u.done
	}
}

// Send enqueues a FeedbackEvent for async processing. Non-blocking if buffer is full.
func (u *ScoreUpdater) Send(ev FeedbackEvent) {
	select {
	case u.ch <- ev:
	default:
		log.Printf("score_updater: buffer full, dropping feedback for %d IDs", len(ev.KnowledgeIDs))
	}
}

func (u *ScoreUpdater) loop() {
	defer close(u.done)
	for ev := range u.ch {
		u.applyWithTimeout(ev)
	}
}

// applyWithTimeout wraps apply with a timeout context, ensuring cancel() is always called.
func (u *ScoreUpdater) applyWithTimeout(ev FeedbackEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	u.apply(ctx, ev)
}

func (u *ScoreUpdater) apply(ctx context.Context, ev FeedbackEvent) {
	if u.qdrant == nil {
		return
	}
	for _, id := range ev.KnowledgeIDs {
		// Fetch current payload to read existing counts.
		results, err := u.qdrant.GetByID(ctx, u.collection, id)
		if err != nil || len(results) == 0 {
			log.Printf("score_updater: get %s failed: %v", id, err)
			continue
		}

		p := results[0].Payload
		usageCount := payloadInt(p, "usage_count") + 1
		successCount := payloadInt(p, "success_count")
		if ev.Outcome {
			successCount++
		}

		effective := (float64(successCount) + 2.0) / (float64(usageCount) + 4.0) // α=2, β=2
		now := time.Now().UTC()

		if err := u.qdrant.UpdatePayload(ctx, u.collection, id, map[string]any{
			"usage_count":   usageCount,
			"success_count": successCount,
			"success_rate":  effective,
			"last_used_at":  now.Format(time.RFC3339),
		}); err != nil {
			log.Printf("score_updater: update %s failed: %v", id, err)
		}
	}
}
