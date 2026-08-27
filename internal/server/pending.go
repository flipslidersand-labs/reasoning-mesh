package server

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const (
	pendingTTL      = 30 * time.Minute
	pendingSweepInt = 10 * time.Minute
	// pendingMaxEntries is the default upper bound on live entries.
	// When exceeded, the oldest entry is evicted before inserting the new one.
	pendingMaxEntries = 10_000
)

// pendingEntry holds knowledge IDs and an expiry time.
type pendingEntry struct {
	knowledgeIDs []string
	expiresAt    time.Time
	insertedAt   time.Time
}

// PendingStore maps request IDs to the knowledge IDs used during inference.
// Clients use the request_id returned by /v1/infer to send feedback without
// having to track knowledge_ids themselves.
//
// Entries are evicted lazily on Get, by the background sweep goroutine started
// in NewPendingStore, and by the cap enforced in Put.
// Call Stop to release the background goroutine.
type PendingStore struct {
	mu      sync.Mutex
	entries map[string]pendingEntry
	maxCap  int
	stopCh  chan struct{}
	once    sync.Once
}

// NewPendingStore creates a PendingStore and starts a background goroutine that
// sweeps expired entries every pendingSweepInt. Call Stop when done.
func NewPendingStore() *PendingStore {
	s := &PendingStore{
		entries: make(map[string]pendingEntry),
		maxCap:  pendingMaxEntries,
		stopCh:  make(chan struct{}),
	}
	go s.sweepLoop()
	return s
}

// Stop terminates the background sweep goroutine. Safe to call multiple times.
func (s *PendingStore) Stop() {
	s.once.Do(func() { close(s.stopCh) })
}

func (s *PendingStore) sweepLoop() {
	ticker := time.NewTicker(pendingSweepInt)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.Sweep()
		case <-s.stopCh:
			return
		}
	}
}

// Put stores knowledge IDs for a new request and returns the request ID.
// If the store has reached maxCap, the oldest entry is evicted first.
func (s *PendingStore) Put(knowledgeIDs []string) string {
	id := newRequestID()
	now := time.Now()
	s.mu.Lock()
	// Evict the oldest entry when at cap to bound memory usage.
	if len(s.entries) >= s.maxCap {
		oldest := ""
		var oldestTime time.Time
		for k, e := range s.entries {
			if oldest == "" || e.insertedAt.Before(oldestTime) {
				oldest = k
				oldestTime = e.insertedAt
			}
		}
		if oldest != "" {
			delete(s.entries, oldest)
		}
	}
	s.entries[id] = pendingEntry{
		knowledgeIDs: knowledgeIDs,
		expiresAt:    now.Add(pendingTTL),
		insertedAt:   now,
	}
	s.mu.Unlock()
	return id
}

// Get returns the knowledge IDs for the given request ID and removes the entry.
// Returns nil if not found or expired.
func (s *PendingStore) Get(requestID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[requestID]
	if !ok || time.Now().After(e.expiresAt) {
		delete(s.entries, requestID)
		return nil
	}
	delete(s.entries, requestID)
	return e.knowledgeIDs
}

// Sweep removes all expired entries. The background goroutine calls this
// automatically; it may also be called directly in tests.
func (s *PendingStore) Sweep() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, e := range s.entries {
		if now.After(e.expiresAt) {
			delete(s.entries, id)
		}
	}
}

func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
