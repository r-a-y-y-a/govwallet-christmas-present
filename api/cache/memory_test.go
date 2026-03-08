package cache

import (
	"context"
	"sync"
	"testing"
)

// Compile-time check: MemoryCache must satisfy CacheStore.
var _ CacheStore = (*MemoryCache)(nil)

func TestMemoryCache_StaffTeam_MissThenHit(t *testing.T) {
	mc := NewMemoryCache()
	ctx := context.Background()

	// Cache miss
	team, found, err := mc.GetStaffTeam(ctx, "STAFF001")
	if err != nil || found || team != "" {
		t.Errorf("expected miss, got found=%v team=%q err=%v", found, team, err)
	}

	// Populate and hit
	if err := mc.SetStaffTeam(ctx, "STAFF001", "Team Alpha"); err != nil {
		t.Fatalf("SetStaffTeam error: %v", err)
	}
	team, found, err = mc.GetStaffTeam(ctx, "STAFF001")
	if err != nil || !found || team != "Team Alpha" {
		t.Errorf("expected hit with 'Team Alpha', got found=%v team=%q err=%v", found, team, err)
	}
}

func TestMemoryCache_StaffTeam_DifferentKeysIsolated(t *testing.T) {
	mc := NewMemoryCache()
	ctx := context.Background()

	_ = mc.SetStaffTeam(ctx, "STAFF001", "Team Alpha")
	_ = mc.SetStaffTeam(ctx, "STAFF002", "Team Beta")

	if team, _, _ := mc.GetStaffTeam(ctx, "STAFF001"); team != "Team Alpha" {
		t.Errorf("expected Team Alpha, got %q", team)
	}
	if team, _, _ := mc.GetStaffTeam(ctx, "STAFF002"); team != "Team Beta" {
		t.Errorf("expected Team Beta, got %q", team)
	}
}

func TestMemoryCache_RedemptionStatus_MissThenNX(t *testing.T) {
	mc := NewMemoryCache()
	ctx := context.Background()

	// Cache miss before any NX
	redeemed, found, err := mc.GetRedemptionStatus(ctx, "Team Alpha")
	if err != nil || found || redeemed {
		t.Errorf("expected miss, got found=%v redeemed=%v err=%v", found, redeemed, err)
	}

	// SETNX first call — should succeed
	set, err := mc.SetRedemptionNX(ctx, "Team Alpha")
	if err != nil || !set {
		t.Fatalf("expected SETNX win, got set=%v err=%v", set, err)
	}

	// Now GetRedemptionStatus should hit redeemed=true
	redeemed, found, err = mc.GetRedemptionStatus(ctx, "Team Alpha")
	if err != nil || !found || !redeemed {
		t.Errorf("expected hit redeemed=true, got found=%v redeemed=%v err=%v", found, redeemed, err)
	}
}

func TestMemoryCache_SetRedemptionNX_SecondCallLoses(t *testing.T) {
	mc := NewMemoryCache()
	ctx := context.Background()

	set1, err := mc.SetRedemptionNX(ctx, "Team Alpha")
	if err != nil || !set1 {
		t.Fatalf("first SETNX should win, got set=%v err=%v", set1, err)
	}

	set2, err := mc.SetRedemptionNX(ctx, "Team Alpha")
	if err != nil || set2 {
		t.Errorf("second SETNX should lose, got set=%v err=%v", set2, err)
	}
}

func TestMemoryCache_InvalidateRedemption(t *testing.T) {
	mc := NewMemoryCache()
	ctx := context.Background()

	_, _ = mc.SetRedemptionNX(ctx, "Team Alpha")

	if err := mc.InvalidateRedemption(ctx, "Team Alpha"); err != nil {
		t.Fatalf("InvalidateRedemption error: %v", err)
	}

	// Should be a miss again after invalidation
	_, found, err := mc.GetRedemptionStatus(ctx, "Team Alpha")
	if err != nil || found {
		t.Errorf("expected miss after invalidation, got found=%v err=%v", found, err)
	}

	// Should be able to SETNX again after invalidation
	set, err := mc.SetRedemptionNX(ctx, "Team Alpha")
	if err != nil || !set {
		t.Errorf("expected SETNX to succeed after invalidation, got set=%v err=%v", set, err)
	}
}

func TestMemoryCache_InvalidateRedemption_NonexistentKeyIsNoop(t *testing.T) {
	mc := NewMemoryCache()
	ctx := context.Background()

	if err := mc.InvalidateRedemption(ctx, "NoSuchTeam"); err != nil {
		t.Errorf("expected no error invalidating missing key, got: %v", err)
	}
}

func TestMemoryCache_Ping(t *testing.T) {
	mc := NewMemoryCache()
	if err := mc.Ping(context.Background()); err != nil {
		t.Errorf("Ping should always return nil for MemoryCache, got: %v", err)
	}
}

func TestMemoryCache_ConcurrentSetRedemptionNX_ExactlyOneWinner(t *testing.T) {
	mc := NewMemoryCache()
	ctx := context.Background()

	const goroutines = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			set, err := mc.SetRedemptionNX(ctx, "Team Alpha")
			if err != nil {
				t.Errorf("SetRedemptionNX error: %v", err)
				return
			}
			if set {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if winners != 1 {
		t.Errorf("expected exactly 1 winner across %d concurrent callers, got %d", goroutines, winners)
	}
}

func TestMemoryCache_ConcurrentSetStaffTeam_LastWriteWins(t *testing.T) {
	mc := NewMemoryCache()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = mc.SetStaffTeam(ctx, "STAFF001", "Team Alpha")
		}(i)
	}
	wg.Wait()

	// After all writes, the key must be readable with no data race
	team, found, err := mc.GetStaffTeam(ctx, "STAFF001")
	if err != nil || !found || team != "Team Alpha" {
		t.Errorf("expected Team Alpha after concurrent writes, got found=%v team=%q err=%v", found, team, err)
	}
}
