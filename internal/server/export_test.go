package server

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
