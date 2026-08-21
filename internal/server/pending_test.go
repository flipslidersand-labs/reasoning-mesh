package server_test

import (
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/server"
)

func TestPendingStore_PutAndGet(t *testing.T) {
	s := server.NewPendingStore()
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
	reqID := s.Put([]string{"x"})

	_ = s.Get(reqID)
	second := s.Get(reqID)
	if second != nil {
		t.Error("second Get should return nil (entry consumed)")
	}
}

func TestPendingStore_UnknownID(t *testing.T) {
	s := server.NewPendingStore()
	got := s.Get("does-not-exist")
	if got != nil {
		t.Errorf("expected nil for unknown ID, got %v", got)
	}
}

func TestPendingStore_UniqueIDs(t *testing.T) {
	s := server.NewPendingStore()
	id1 := s.Put([]string{"a"})
	id2 := s.Put([]string{"b"})
	if id1 == id2 {
		t.Error("consecutive Put calls should produce distinct IDs")
	}
}

func TestPendingStore_EmptyKnowledgeIDs(t *testing.T) {
	s := server.NewPendingStore()
	reqID := s.Put(nil)
	got := s.Get(reqID)
	// nil stored → nil returned (no crash)
	if got != nil {
		t.Errorf("expected nil for empty knowledge IDs, got %v", got)
	}
}
