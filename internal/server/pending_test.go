package server_test

import (
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/server"
)

func TestPendingStore_PutAndGet(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	ids := []string{"abc", "def"}
	reqID := s.Put(ids)

	if reqID == "" {
		t.Fatal("Put should return non-empty request ID")
	}

	got := s.Get(reqID)
	if len(got) != 2 || got[0] != "abc" || got[1] != "def" {
		t.Errorf("Get returned %v, want %v", got, ids)
	}
}

func TestPendingStore_GetConsumesEntry(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	reqID := s.Put([]string{"x"})

	_ = s.Get(reqID)
	second := s.Get(reqID)
	if second != nil {
		t.Error("second Get should return nil (entry consumed)")
	}
}

func TestPendingStore_UnknownID(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	got := s.Get("does-not-exist")
	if got != nil {
		t.Errorf("expected nil for unknown ID, got %v", got)
	}
}

func TestPendingStore_UniqueIDs(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	id1 := s.Put([]string{"a"})
	id2 := s.Put([]string{"b"})
	if id1 == id2 {
		t.Error("consecutive Put calls should produce distinct IDs")
	}
}

func TestPendingStore_EmptyKnowledgeIDs(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	reqID := s.Put(nil)
	got := s.Get(reqID)
	// nil stored → nil returned (no crash)
	if got != nil {
		t.Errorf("expected nil for empty knowledge IDs, got %v", got)
	}
}

func TestPendingStore_StopIdempotent(t *testing.T) {
	s := server.NewPendingStore()
	// Calling Stop multiple times must not panic.
	s.Stop()
	s.Stop()
}

func TestPendingStore_CapEvictsOldest(t *testing.T) {
	const smallCap = 5
	s := server.NewPendingStoreWithCap(smallCap)
	defer s.Stop()

	ids := make([]string, smallCap+1)
	for i := range ids {
		ids[i] = s.Put([]string{"k"})
	}

	// The first inserted ID should have been evicted.
	if got := s.Get(ids[0]); got != nil {
		t.Error("oldest entry should have been evicted when cap was exceeded")
	}

	// All others should still be present.
	for _, id := range ids[1:] {
		if got := s.Get(id); got == nil {
			t.Errorf("entry %s should still exist", id)
		}
	}
}
