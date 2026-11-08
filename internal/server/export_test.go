package server

import "time"

// NewPendingStoreWithCap is a test-only constructor that creates a PendingStore
// with a custom entry cap, allowing cap-eviction logic to be tested without
// inserting the full 10 000 entries required by the production default.
func NewPendingStoreWithCap(cap int) *PendingStore {
	s := &PendingStore{
		entries: make(map[string]pendingEntry),
		maxCap:  cap,
		stopCh:  make(chan struct{}),
	}
	go s.sweepLoop()
	return s
}

// Len returns the number of entries currently held (expired or not).
func (s *PendingStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// SetExpiry overrides the expiry of an existing entry. It reports whether the
// entry existed. Tests use it instead of sleeping past the production TTL.
func (s *PendingStore) SetExpiry(id string, t time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return false
	}
	e.expiresAt = t
	s.entries[id] = e
	return true
}

// SetNow injects a clock used for expiry decisions and TTL stamping.
func (s *PendingStore) SetNow(f func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = f
}

// PendingTTL exposes the production TTL to tests.
const PendingTTL = pendingTTL
