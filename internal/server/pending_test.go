package server_test

import (
	"sync"
	"testing"
	"time"

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

func TestPendingStore_RestoreAfterGet(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	reqID := s.Put([]string{"x"})

	ids := s.Get(reqID)
	s.Restore(reqID, ids)

	if got := s.Get(reqID); len(got) != 1 || got[0] != "x" {
		t.Errorf("Get after Restore = %v, want [x]", got)
	}
	if s.Get(reqID) != nil {
		t.Error("entry should be consumed again after the second Get")
	}
}

func TestPendingStore_ExpiredGet(t *testing.T) {
	tests := []struct {
		name   string
		offset time.Duration // expiry relative to now
		want   bool          // Get returns IDs
	}{
		{"far future is valid", time.Hour, true},
		{"just expired", -time.Nanosecond, false},
		{"long expired", -time.Hour, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := server.NewPendingStore()
			defer s.Stop()
			id := s.Put([]string{"k"})
			if !s.SetExpiry(id, time.Now().Add(tt.offset)) {
				t.Fatal("entry not found")
			}
			got := s.Get(id)
			if (got != nil) != tt.want {
				t.Errorf("Get = %v, want non-nil=%v", got, tt.want)
			}
			if s.Len() != 0 {
				t.Errorf("Len = %d, want 0 (Get removes entry even when expired)", s.Len())
			}
		})
	}
}

func TestPendingStore_SweepMixed(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()

	var valid, expired []string
	for i := 0; i < 3; i++ {
		valid = append(valid, s.Put([]string{"v"}))
		id := s.Put([]string{"e"})
		if !s.SetExpiry(id, time.Now().Add(-time.Minute)) {
			t.Fatal("entry not found")
		}
		expired = append(expired, id)
	}
	if s.Len() != 6 {
		t.Fatalf("Len before sweep = %d, want 6", s.Len())
	}

	s.Sweep()

	if s.Len() != 3 {
		t.Errorf("Len after sweep = %d, want 3", s.Len())
	}
	// Note: Get itself returns nil for expired entries, so this loop does not
	// verify Sweep; the Len == 3 check above is what proves Sweep removed them.
	for _, id := range expired {
		if s.Get(id) != nil {
			t.Errorf("expired %s should be nil after Sweep", id)
		}
	}
	for _, id := range valid {
		if got := s.Get(id); len(got) != 1 || got[0] != "v" {
			t.Errorf("valid %s Get = %v, want [v]", id, got)
		}
	}
}

func TestPendingStore_SweepEmptyAndAllValid(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	s.Sweep() // empty store must not panic
	s.Put([]string{"a"})
	s.Sweep()
	if s.Len() != 1 {
		t.Errorf("Len = %d, want 1 (valid entries survive Sweep)", s.Len())
	}
}

func TestPendingStore_ConcurrentAddGetSweep(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				id := s.Put([]string{"k"})
				if i%2 == 0 {
					if !s.SetExpiry(id, time.Now().Add(-time.Second)) {
						t.Errorf("entry %s not found", id)
					}
				}
				s.Sweep()
				if got := s.Get(id); got != nil && i%2 == 0 {
					t.Errorf("expired entry returned %v", got)
				}
				s.Restore(id, []string{"r"})
				_ = s.Len()
			}
		}()
	}
	wg.Wait()
}

// fixedNow is the injected clock used by the expiresAt boundary tests.
var fixedNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// expiresAt == now is still valid (expiry is strictly "now after expiresAt").
func TestPendingStore_Get_ExpiresAtEqualsNow_IsValid(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	s.SetNow(func() time.Time { return fixedNow })
	id := s.Put([]string{"k"})
	s.SetExpiry(id, fixedNow)
	if got := s.Get(id); len(got) != 1 || got[0] != "k" {
		t.Errorf("Get at expiresAt==now = %v, want [k]", got)
	}
}

func TestPendingStore_Get_OneNsPastExpiry_IsExpired(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	s.SetNow(func() time.Time { return fixedNow })
	id := s.Put([]string{"k"})
	s.SetExpiry(id, fixedNow.Add(-time.Nanosecond))
	if got := s.Get(id); got != nil {
		t.Errorf("Get 1ns past expiry = %v, want nil", got)
	}
}

func TestPendingStore_Sweep_ExpiresAtEqualsNow_Kept(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	s.SetNow(func() time.Time { return fixedNow })
	keep := s.Put([]string{"keep"})
	drop := s.Put([]string{"drop"})
	s.SetExpiry(keep, fixedNow)
	s.SetExpiry(drop, fixedNow.Add(-time.Nanosecond))
	s.Sweep()
	if s.Len() != 1 {
		t.Fatalf("Len after Sweep = %d, want 1", s.Len())
	}
	if got := s.Get(keep); len(got) != 1 {
		t.Errorf("entry with expiresAt==now was swept: %v", got)
	}
}
