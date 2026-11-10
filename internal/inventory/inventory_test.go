package inventory

import (
	"testing"
	"time"
)

type FakeClock struct {
	current time.Time
}

func (f *FakeClock) Now() time.Time {
	return f.current
}

func (f *FakeClock) Add(d time.Duration) {
	f.current = f.current.Add(d)
}

func TestService_Reserve(t *testing.T) {
	tests := []struct {
		name          string
		stock         int
		quantity      int
		wait         time.Duration
		wantErr      error
	}{
		{
			name:     "reserve successfully",
			stock:    10,
			quantity: 5,
			wantErr:  nil,
		},
		{
			name:     "insufficient stock",
			stock:    5,
			quantity: 10,
			wantErr:  ErrInsufficientStock,
		},
		{
			name:     "exact stock",
			stock:    5,
			quantity: 5,
			wantErr:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &FakeClock{current: time.Now()}
			svc := NewService(clock, 5*time.Minute)
			svc.SetStock("var001", tt.stock)

			reservation, err := svc.Reserve("res001", "var001", tt.quantity)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("Reserve() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("Reserve() unexpected error: %v", err)
				return
			}

			if reservation.Quantity != tt.quantity {
				t.Errorf("Reserve() quantity = %d, want %d", reservation.Quantity, tt.quantity)
			}
		})
	}
}

func TestService_Release(t *testing.T) {
	clock := &FakeClock{current: time.Now()}
	svc := NewService(clock, 5*time.Minute)
	svc.SetStock("var001", 10)

	_, err := svc.Reserve("res001", "var001", 5)
	if err != nil {
		t.Fatalf("Reserve() error: %v", err)
	}

	err = svc.Release("res001")
	if err != nil {
		t.Errorf("Release() error: %v", err)
	}

	_, err = svc.Reserve("res001", "var001", 10)
	if err != nil {
		t.Errorf("Release() should free stock: %v", err)
	}

	err = svc.Release("res999")
	if err != ErrReservationNotFound {
		t.Errorf("Release() error = %v, want %v", err, ErrReservationNotFound)
	}
}

func TestService_ExpiredReservation(t *testing.T) {
	clock := &FakeClock{current: time.Now()}
	svc := NewService(clock, 5*time.Minute)
	svc.SetStock("var001", 10)

	_, err := svc.Reserve("res001", "var001", 8)
	if err != nil {
		t.Fatalf("Reserve() error: %v", err)
	}

	_, err = svc.Reserve("res002", "var001", 3)
	if err != ErrInsufficientStock {
		t.Errorf("Reserve() should fail with insufficient stock: %v", err)
	}

	clock.Add(6 * time.Minute)

	_, err = svc.GetReservation("res001")
	if err != ErrReservationExpired {
		t.Errorf("GetReservation() error = %v, want %v", err, ErrReservationExpired)
	}

	_, err = svc.Reserve("res002", "var001", 3)
	if err != nil {
		t.Errorf("Reserve() should succeed after expiration: %v", err)
	}
}

func TestService_CleanupExpired(t *testing.T) {
	clock := &FakeClock{current: time.Now()}
	svc := NewService(clock, 5*time.Minute)
	svc.SetStock("var001", 10)

	_, err := svc.Reserve("res001", "var001", 5)
	if err != nil {
		t.Fatalf("Reserve() error: %v", err)
	}

	clock.Add(6 * time.Minute)

	svc.CleanupExpired()

	if len(svc.reservations) != 0 {
		t.Errorf("CleanupExpired() reservations = %d, want 0", len(svc.reservations))
	}
}
