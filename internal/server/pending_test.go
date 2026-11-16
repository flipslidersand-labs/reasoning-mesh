package server_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/server"
)

// claimCommit claims the entry and, if it was granted, commits it (removing it).
// Returns nil when the entry is missing, expired or busy.
func claimCommit(s *server.PendingStore, id string) []string {
	ids, busy := s.Claim(id)
	if busy || ids == nil {
		return nil
	}
	s.Commit(id)
	return ids
}

func TestPendingStore_PutAndClaimCommit(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	ids := []string{"abc", "def"}
	reqID := s.Put(ids)

	if reqID == "" {
		t.Fatal("Put should return non-empty request ID")
	}

	got := claimCommit(s, reqID)
	if len(got) != 2 || got[0] != "abc" || got[1] != "def" {
		t.Errorf("claim returned %v, want %v", got, ids)
	}
}

func TestPendingStore_CommitConsumesEntry(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	reqID := s.Put([]string{"x"})

	_ = claimCommit(s, reqID)
	second := claimCommit(s, reqID)
	if second != nil {
		t.Error("second claim should return nil (entry consumed)")
	}
}

func TestPendingStore_UnknownID(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	got := claimCommit(s, "does-not-exist")
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
	got := claimCommit(s, reqID)
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
	if got := claimCommit(s, ids[0]); got != nil {
		t.Error("oldest entry should have been evicted when cap was exceeded")
	}

	// All others should still be present.
	for _, id := range ids[1:] {
		if got := claimCommit(s, id); got == nil {
			t.Errorf("entry %s should still exist", id)
		}
	}
}

func TestPendingStore_ExpiredGet(t *testing.T) {
	tests := []struct {
		name   string
		offset time.Duration // expiry relative to now
		want   bool          // Claim returns IDs
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
			got := claimCommit(s, id)
			if (got != nil) != tt.want {
				t.Errorf("Claim = %v, want non-nil=%v", got, tt.want)
			}
			if s.Len() != 0 {
				t.Errorf("Len = %d, want 0 (claim+commit removes entry even when expired)", s.Len())
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
	// Note: Claim itself returns nil for expired entries, so this loop does not
	// verify Sweep; the Len == 3 check above is what proves Sweep removed them.
	for _, id := range expired {
		if claimCommit(s, id) != nil {
			t.Errorf("expired %s should be nil after Sweep", id)
		}
	}
	for _, id := range valid {
		if got := claimCommit(s, id); len(got) != 1 || got[0] != "v" {
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
				if got := claimCommit(s, id); got != nil && i%2 == 0 {
					t.Errorf("expired entry returned %v", got)
				}
				_, _ = s.Claim(id)
				s.Release(id)
				_ = s.Len()
			}
		}()
	}
	wg.Wait()
}

// fixedNow is the injected clock used by the expiresAt boundary tests.
var fixedNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// expiresAt == now is still valid (expiry is strictly "now after expiresAt").
func TestPendingStore_Claim_ExpiresAtEqualsNow_IsValid(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	s.SetNow(func() time.Time { return fixedNow })
	id := s.Put([]string{"k"})
	s.SetExpiry(id, fixedNow)
	if got := claimCommit(s, id); len(got) != 1 || got[0] != "k" {
		t.Errorf("Claim at expiresAt==now = %v, want [k]", got)
	}
}

func TestPendingStore_Claim_OneNsPastExpiry_IsExpired(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	s.SetNow(func() time.Time { return fixedNow })
	id := s.Put([]string{"k"})
	s.SetExpiry(id, fixedNow.Add(-time.Nanosecond))
	if got := claimCommit(s, id); got != nil {
		t.Errorf("Claim 1ns past expiry = %v, want nil", got)
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
		t.Fatalf("Len after Sweep = %d, want 1 (expiresAt==now kept, 1ns past swept)", s.Len())
	}
}

// Put stamps expiresAt from the injected clock: valid at exactly TTL, expired
// 1ns later.
func TestPendingStore_Put_TTLStampedFromInjectedClock(t *testing.T) {
	for _, tc := range []struct {
		name    string
		advance time.Duration
		valid   bool
	}{
		{"at TTL", server.PendingTTL, true},
		{"TTL+1ns", server.PendingTTL + time.Nanosecond, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := server.NewPendingStore()
			defer s.Stop()
			cur := fixedNow
			s.SetNow(func() time.Time { return cur })
			id := s.Put([]string{"k"})
			cur = fixedNow.Add(tc.advance)
			got := claimCommit(s, id)
			if tc.valid && len(got) != 1 {
				t.Errorf("Claim = %v, want [k]", got)
			}
			if !tc.valid && got != nil {
				t.Errorf("Claim = %v, want nil", got)
			}
		})
	}
}

func TestPendingStore_Claim_ConcurrentExactlyOneOK(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	id := s.Put([]string{"x"})
	const n = 32
	var ok, busy atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ids, b := s.Claim(id)
			switch {
			case b:
				busy.Add(1)
			case len(ids) == 1:
				ok.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if ok.Load() != 1 || busy.Load() != n-1 {
		t.Fatalf("ok=%d busy=%d, want 1 and %d", ok.Load(), busy.Load(), n-1)
	}
}

func TestPendingStore_Release_KeepsOriginalTTL(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	cur := fixedNow
	s.SetNow(func() time.Time { return cur })
	id := s.Put([]string{"x"})

	cur = fixedNow.Add(20 * time.Minute)
	if ids, busy := s.Claim(id); busy || len(ids) != 1 {
		t.Fatalf("claim at 20m = %v,%v", ids, busy)
	}
	s.Release(id)

	cur = fixedNow.Add(31 * time.Minute)
	if ids, busy := s.Claim(id); busy || ids != nil {
		t.Fatalf("claim at 31m = %v,%v, want not found (TTL not extended)", ids, busy)
	}
	if s.Len() != 0 {
		t.Errorf("Len = %d, want 0 (expired entry removed)", s.Len())
	}
}

func TestPendingStore_Release_AtCapDoesNotChangeStore(t *testing.T) {
	s := server.NewPendingStoreWithCap(3)
	defer s.Stop()
	cur := fixedNow
	s.SetNow(func() time.Time { return cur })
	ids := make([]string, 3)
	for i, v := range []string{"a", "b", "c"} {
		ids[i] = s.Put([]string{v})
		cur = cur.Add(time.Minute) // distinct insertedAt and expiresAt per entry
	}
	// ids[0]: inserted at fixedNow, expires at fixedNow+TTL.
	if got, _ := s.Claim(ids[0]); got == nil {
		t.Fatal("claim failed")
	}
	cur = cur.Add(5 * time.Minute)
	s.Release(ids[0])
	if s.Len() != 3 {
		t.Fatalf("Len = %d, want 3", s.Len())
	}

	// Just past the original expiry of ids[0] (a re-stamped TTL would still be live),
	// while ids[1] and ids[2] are still within theirs.
	cur = fixedNow.Add(server.PendingTTL + time.Nanosecond)
	if got, busy := s.Claim(ids[0]); got != nil || busy {
		t.Errorf("released entry must keep original expiry, claim = %v,%v", got, busy)
	}
	for i, want := range map[int]string{1: "b", 2: "c"} {
		got, busy := s.Claim(ids[i])
		if busy || len(got) != 1 || got[0] != want {
			t.Errorf("entry %d lost or altered after Release: %v,%v", i, got, busy)
		}
	}
}

func TestPendingStore_Put_DoesNotEvictInflight(t *testing.T) {
	s := server.NewPendingStoreWithCap(3)
	defer s.Stop()
	cur := fixedNow
	s.SetNow(func() time.Time { cur = cur.Add(time.Second); return cur })
	oldest := s.Put([]string{"a"})
	second := s.Put([]string{"b"})
	third := s.Put([]string{"c"})
	if got, _ := s.Claim(oldest); got == nil {
		t.Fatal("claim failed")
	}
	s.Put([]string{"d"})
	if got, busy := s.Claim(second); got != nil || busy {
		t.Errorf("second-oldest should have been evicted, got %v,%v", got, busy)
	}
	if _, busy := s.Claim(oldest); !busy {
		t.Error("inflight oldest must survive eviction")
	}
	if got, _ := s.Claim(third); got == nil {
		t.Error("third should remain")
	}
}

func TestPendingStore_Release_DoesNotResurrect(t *testing.T) {
	s := server.NewPendingStore()
	defer s.Stop()
	cur := fixedNow
	s.SetNow(func() time.Time { return cur })

	swept := s.Put([]string{"a"})
	_, _ = s.Claim(swept)
	cur = fixedNow.Add(time.Hour)
	s.Sweep()
	s.Release(swept)

	committed := s.Put([]string{"b"})
	_, _ = s.Claim(committed)
	s.Commit(committed)
	s.Release(committed)

	if s.Len() != 0 {
		t.Fatalf("Len = %d, want 0", s.Len())
	}
	if ids, busy := s.Claim(committed); ids != nil || busy {
		t.Errorf("committed entry resurrected: %v,%v", ids, busy)
	}
}
