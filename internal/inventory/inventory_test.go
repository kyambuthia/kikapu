package inventory

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLevel_Available(t *testing.T) {
	tests := []struct {
		name     string
		onHand   int
		reserved int
		want     int
	}{
		{"normal", 100, 20, 80},
		{"fully reserved", 50, 50, 0},
		{"no reservations", 100, 0, 100},
		{"over reserved (should not happen)", 50, 75, -25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := Level{OnHand: tt.onHand, Reserved: tt.reserved}
			if got := l.Available(); got != tt.want {
				t.Errorf("Available() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMemoryStore_Reserve(t *testing.T) {
	ctx := context.Background()
	clock := &FakeClock{Current: time.Now()}
	store := NewMemoryStore(clock)
	store.SetLevel("v1", "loc1", 100)

	tests := []struct {
		name    string
		qty     int
		wantErr error
	}{
		{"reserve successfully", 10, nil},
		{"reserve more", 50, nil},
		{"insufficient stock", 100, ErrInsufficientStock},
		{"reserve exactly available", 40, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := store.Reserve(ctx, "v1", tt.qty, time.Minute)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("Reserve() error = %v, want %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("Reserve() unexpected error: %v", err)
				return
			}

			if res.Qty != tt.qty {
				t.Errorf("Reserve() qty = %d, want %d", res.Qty, tt.qty)
			}

			if res.VariantID != "v1" {
				t.Errorf("Reserve() variantID = %s, want v1", res.VariantID)
			}
		})
	}
}

func TestMemoryStore_ReserveNeverOversells(t *testing.T) {
	ctx := context.Background()
	clock := &FakeClock{Current: time.Now()}
	store := NewMemoryStore(clock)
	store.SetLevel("v1", "loc1", 10)

	var wg sync.WaitGroup
	var ok atomic.Int64

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.Reserve(ctx, "v1", 1, time.Minute); err == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := ok.Load(); got != 10 {
		t.Fatalf("sold %d, want 10", got)
	}
}

func TestMemoryStore_Release(t *testing.T) {
	ctx := context.Background()
	clock := &FakeClock{Current: time.Now()}
	store := NewMemoryStore(clock)
	store.SetLevel("v1", "loc1", 10)

	res, err := store.Reserve(ctx, "v1", 5, time.Minute)
	if err != nil {
		t.Fatalf("Reserve() error: %v", err)
	}

	err = store.Release(ctx, res.ID)
	if err != nil {
		t.Errorf("Release() error: %v", err)
	}

	_, err = store.Reserve(ctx, "v1", 10, time.Minute)
	if err != nil {
		t.Errorf("Release() should free stock: %v", err)
	}

	err = store.Release(ctx, "nonexistent")
	if err != ErrReservationNotFound {
		t.Errorf("Release() error = %v, want %v", err, ErrReservationNotFound)
	}
}

func TestMemoryStore_Commit(t *testing.T) {
	ctx := context.Background()
	clock := &FakeClock{Current: time.Now()}
	store := NewMemoryStore(clock)
	store.SetLevel("v1", "loc1", 10)

	res, err := store.Reserve(ctx, "v1", 5, time.Minute)
	if err != nil {
		t.Fatalf("Reserve() error: %v", err)
	}

	err = store.Commit(ctx, res.ID)
	if err != nil {
		t.Errorf("Commit() error: %v", err)
	}

	levels, err := store.Levels(ctx, "v1")
	if err != nil {
		t.Fatalf("Levels() error: %v", err)
	}
	if len(levels) == 0 {
		t.Fatal("Levels() returned empty")
	}
	if levels[0].OnHand != 5 {
		t.Errorf("OnHand = %d, want 5", levels[0].OnHand)
	}
	if levels[0].Reserved != 0 {
		t.Errorf("Reserved = %d, want 0", levels[0].Reserved)
	}

	err = store.Commit(ctx, "nonexistent")
	if err != ErrReservationNotFound {
		t.Errorf("Commit() error = %v, want %v", err, ErrReservationNotFound)
	}
}

func TestMemoryStore_CommitExpired(t *testing.T) {
	ctx := context.Background()
	clock := &FakeClock{Current: time.Now()}
	store := NewMemoryStore(clock)
	store.SetLevel("v1", "loc1", 10)

	res, err := store.Reserve(ctx, "v1", 5, time.Minute)
	if err != nil {
		t.Fatalf("Reserve() error: %v", err)
	}

	clock.Add(2 * time.Minute)

	err = store.Commit(ctx, res.ID)
	if err != ErrReservationExpired {
		t.Errorf("Commit() error = %v, want %v", err, ErrReservationExpired)
	}

	levels, _ := store.Levels(ctx, "v1")
	if levels[0].Reserved != 0 {
		t.Errorf("Expired reservation should free reserved stock, got %d", levels[0].Reserved)
	}
}

func TestMemoryStore_ExpireDue(t *testing.T) {
	ctx := context.Background()
	clock := &FakeClock{Current: time.Now()}
	store := NewMemoryStore(clock)
	store.SetLevel("v1", "loc1", 10)

	store.Reserve(ctx, "v1", 3, time.Minute)
	store.Reserve(ctx, "v1", 2, time.Minute)

	clock.Add(2 * time.Minute)

	count, err := store.ExpireDue(ctx, clock.Now())
	if err != nil {
		t.Fatalf("ExpireDue() error: %v", err)
	}
	if count != 2 {
		t.Errorf("ExpireDue() count = %d, want 2", count)
	}

	levels, _ := store.Levels(ctx, "v1")
	if levels[0].Reserved != 0 {
		t.Errorf("Reserved = %d, want 0 after expiry", levels[0].Reserved)
	}
}

func TestMemoryStore_MultipleLocations(t *testing.T) {
	ctx := context.Background()
	clock := &FakeClock{Current: time.Now()}
	store := NewMemoryStore(clock)
	store.SetLevel("v1", "loc1", 10)
	store.SetLevel("v1", "loc2", 20)

	res, err := store.Reserve(ctx, "v1", 15, time.Minute)
	if err != nil {
		t.Fatalf("Reserve() error: %v", err)
	}

	if res.LocationID != "loc2" {
		t.Errorf("Should reserve from location with more stock, got %s", res.LocationID)
	}

	levels, _ := store.Levels(ctx, "v1")
	for _, l := range levels {
		if l.LocationID == "loc2" {
			if l.Reserved != 15 {
				t.Errorf("loc2 reserved = %d, want 15", l.Reserved)
			}
		} else if l.LocationID == "loc1" {
			if l.Reserved != 0 {
				t.Errorf("loc1 reserved = %d, want 0", l.Reserved)
			}
		}
	}
}

func TestMemoryStore_ReservesFromSmallestSufficientLocation(t *testing.T) {
	ctx := context.Background()
	clock := &FakeClock{Current: time.Now()}
	store := NewMemoryStore(clock)
	store.SetLevel("v1", "loc1", 100)
	store.SetLevel("v1", "loc2", 20)

	res, err := store.Reserve(ctx, "v1", 15, time.Minute)
	if err != nil {
		t.Fatalf("Reserve() error: %v", err)
	}

	if res.LocationID != "loc2" {
		t.Errorf("Should reserve from smallest sufficient location, got %s", res.LocationID)
	}
}

func TestMemoryStore_Levels(t *testing.T) {
	ctx := context.Background()
	clock := &FakeClock{Current: time.Now()}
	store := NewMemoryStore(clock)

	levels, err := store.Levels(ctx, "nonexistent")
	if err != nil {
		t.Errorf("Levels() error: %v", err)
	}
	if levels != nil {
		t.Errorf("Levels() should return nil for nonexistent variant")
	}

	store.SetLevel("v1", "loc1", 10)
	store.SetLevel("v1", "loc2", 20)

	levels, err = store.Levels(ctx, "v1")
	if err != nil {
		t.Fatalf("Levels() error: %v", err)
	}
	if len(levels) != 2 {
		t.Errorf("Levels() returned %d levels, want 2", len(levels))
	}
}

func TestMemoryStore_ConcurrentReserveCommit(t *testing.T) {
	ctx := context.Background()
	clock := &FakeClock{Current: time.Now()}
	store := NewMemoryStore(clock)
	store.SetLevel("v1", "loc1", 100)

	var wg sync.WaitGroup
	var committed atomic.Int32
	var released atomic.Int32

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(commit bool) {
			defer wg.Done()
			res, err := store.Reserve(ctx, "v1", 1, time.Minute)
			if err != nil {
				return
			}

			if commit {
				if store.Commit(ctx, res.ID) == nil {
					committed.Add(1)
				}
			} else {
				if store.Release(ctx, res.ID) == nil {
					released.Add(1)
				}
			}
		}(i%2 == 0)
	}
	wg.Wait()

	levels, _ := store.Levels(ctx, "v1")
	committedCount := int(committed.Load())
	expectedOnHand := 100 - committedCount
	if levels[0].OnHand != expectedOnHand {
		t.Errorf("OnHand = %d, want %d", levels[0].OnHand, expectedOnHand)
	}
	if levels[0].Reserved != 0 {
		t.Errorf("Reserved = %d, want 0 (all reservations should be committed or released)", levels[0].Reserved)
	}
}

func TestMemoryStore_DoubleOperations(t *testing.T) {
	ctx := context.Background()
	clock := &FakeClock{Current: time.Now()}
	store := NewMemoryStore(clock)
	store.SetLevel("v1", "loc1", 10)

	res, _ := store.Reserve(ctx, "v1", 5, time.Minute)

	store.Commit(ctx, res.ID)
	err := store.Commit(ctx, res.ID)
	if err != ErrReservationNotFound {
		t.Errorf("Second commit should fail with ErrReservationNotFound, got %v", err)
	}

	res2, _ := store.Reserve(ctx, "v1", 2, time.Minute)
	store.Release(ctx, res2.ID)
	err = store.Release(ctx, res2.ID)
	if err != ErrReservationNotFound {
		t.Errorf("Second release should fail with ErrReservationNotFound, got %v", err)
	}
}

func TestMemoryStore_ReleaseNonExistentLocation(t *testing.T) {
	ctx := context.Background()
	clock := &FakeClock{Current: time.Now()}
	store := NewMemoryStore(clock)

	res := &Reservation{
		ID:         "test-res",
		VariantID:  "v1",
		LocationID: "loc1",
		Qty:        5,
		ExpiresAt:  clock.Now().Add(time.Minute),
		CreatedAt:  clock.Now(),
	}
	store.reservations[res.ID] = res

	err := store.Release(ctx, res.ID)
	if err != nil {
		t.Errorf("Release() should handle missing location gracefully: %v", err)
	}
}
