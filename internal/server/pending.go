package server

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const pendingTTL = 30 * time.Minute

// pendingEntry holds knowledge IDs and an expiry time.
type pendingEntry struct {
	knowledgeIDs []string
	expiresAt    time.Time
}

// PendingStore maps request IDs to the knowledge IDs used during inference.
// Clients use the request_id returned by /v1/infer to send feedback without
// having to track knowledge_ids themselves.
//
// Entries are evicted lazily on Get or by periodic Sweep calls.
type PendingStore struct {
	mu      sync.Mutex
	entries map[string]pendingEntry
}

// NewPendingStore creates a PendingStore.
func NewPendingStore() *PendingStore {
	return &PendingStore{entries: make(map[string]pendingEntry)}
}

// Put stores knowledge IDs for a new request and returns the request ID.
func (s *PendingStore) Put(knowledgeIDs []string) string {
	id := newRequestID()
	s.mu.Lock()
	s.entries[id] = pendingEntry{
		knowledgeIDs: knowledgeIDs,
		expiresAt:    time.Now().Add(pendingTTL),
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

// Sweep removes all expired entries. Call periodically to prevent unbounded growth.
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
