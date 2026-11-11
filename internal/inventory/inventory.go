// Package inventory provides stock management with location-aware inventory levels
// and atomic reservations that prevent overselling under concurrent access.
//
// Reservation Lifecycle:
//
// A reservation follows one of three paths:
//  1. reserve -> commit  (sale completed, stock removed)
//  2. reserve -> release (customer cancelled, stock returned)
//  3. reserve -> expire  (timeout, stock returned automatically)
//
// The checkout flow enforces this ordering:
//   - Reserve inventory BEFORE charging payment
//   - Commit on successful capture
//   - Release on failure or cancellation
//   - Expiry handled by background process
//
// This contract ensures inventory is never oversold, even under high concurrency.
package inventory

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrInsufficientStock    = errors.New("inventory: insufficient stock")
	ErrReservationNotFound  = errors.New("inventory: reservation not found")
	ErrReservationExpired   = errors.New("inventory: reservation expired")
	ErrReservationNotActive = errors.New("inventory: reservation not active")
)

type Level struct {
	VariantID  string
	LocationID string
	OnHand     int
	Reserved   int
}

func (l Level) Available() int {
	return l.OnHand - l.Reserved
}

type Reservation struct {
	ID         string
	VariantID  string
	LocationID string
	Qty        int
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

type Store interface {
	Levels(ctx context.Context, variantID string) ([]Level, error)
	Reserve(ctx context.Context, variantID string, qty int, ttl time.Duration) (Reservation, error)
	Release(ctx context.Context, reservationID string) error
	Commit(ctx context.Context, reservationID string) error
	ExpireDue(ctx context.Context, now time.Time) (int, error)
}

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

type FakeClock struct {
	Current time.Time
}

func (f *FakeClock) Now() time.Time { return f.Current }

func (f *FakeClock) Add(d time.Duration) { f.Current = f.Current.Add(d) }

type MemoryStore struct {
	mu           sync.Mutex
	levels       map[string]map[string]*Level
	reservations map[string]*Reservation
	clock        Clock
	now          func() time.Time
}

func NewMemoryStore(clock Clock) *MemoryStore {
	if clock == nil {
		clock = RealClock{}
	}
	return &MemoryStore{
		levels:       make(map[string]map[string]*Level),
		reservations: make(map[string]*Reservation),
		clock:        clock,
		now:          clock.Now,
	}
}

func (s *MemoryStore) SetLevel(variantID, locationID string, onHand int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.levels[variantID] == nil {
		s.levels[variantID] = make(map[string]*Level)
	}
	s.levels[variantID][locationID] = &Level{
		VariantID:  variantID,
		LocationID: locationID,
		OnHand:     onHand,
		Reserved:   0,
	}
}

func (s *MemoryStore) Levels(ctx context.Context, variantID string) ([]Level, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	locMap, ok := s.levels[variantID]
	if !ok {
		return nil, nil
	}

	result := make([]Level, 0, len(locMap))
	for _, l := range locMap {
		result = append(result, Level{
			VariantID:  l.VariantID,
			LocationID: l.LocationID,
			OnHand:     l.OnHand,
			Reserved:   l.Reserved,
		})
	}
	return result, nil
}

func (s *MemoryStore) Reserve(ctx context.Context, variantID string, qty int, ttl time.Duration) (Reservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	locMap, ok := s.levels[variantID]
	if !ok {
		return Reservation{}, ErrInsufficientStock
	}

	var bestLocation *Level
	for _, l := range locMap {
		available := l.OnHand - l.Reserved
		if available >= qty {
			if bestLocation == nil || available < (bestLocation.OnHand-bestLocation.Reserved) {
				bestLocation = l
			}
		}
	}

	if bestLocation == nil {
		return Reservation{}, ErrInsufficientStock
	}

	now := s.now()
	reservation := &Reservation{
		ID:         generateID(),
		VariantID:  variantID,
		LocationID: bestLocation.LocationID,
		Qty:        qty,
		ExpiresAt:  now.Add(ttl),
		CreatedAt:  now,
	}

	bestLocation.Reserved += qty
	s.reservations[reservation.ID] = reservation

	return Reservation{
		ID:         reservation.ID,
		VariantID:  reservation.VariantID,
		LocationID: reservation.LocationID,
		Qty:        reservation.Qty,
		ExpiresAt:  reservation.ExpiresAt,
		CreatedAt:  reservation.CreatedAt,
	}, nil
}

func (s *MemoryStore) Release(ctx context.Context, reservationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, ok := s.reservations[reservationID]
	if !ok {
		return ErrReservationNotFound
	}

	locMap, ok := s.levels[res.VariantID]
	if !ok {
		delete(s.reservations, reservationID)
		return nil
	}

	loc, ok := locMap[res.LocationID]
	if ok && loc.Reserved >= res.Qty {
		loc.Reserved -= res.Qty
	}

	delete(s.reservations, reservationID)
	return nil
}

func (s *MemoryStore) Commit(ctx context.Context, reservationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, ok := s.reservations[reservationID]
	if !ok {
		return ErrReservationNotFound
	}

	now := s.now()
	if res.ExpiresAt.Before(now) {
		locMap, ok := s.levels[res.VariantID]
		if ok {
			loc, ok := locMap[res.LocationID]
			if ok && loc.Reserved >= res.Qty {
				loc.Reserved -= res.Qty
			}
		}
		delete(s.reservations, reservationID)
		return ErrReservationExpired
	}

	locMap, ok := s.levels[res.VariantID]
	if !ok {
		delete(s.reservations, reservationID)
		return ErrReservationNotFound
	}

	loc, ok := locMap[res.LocationID]
	if !ok {
		delete(s.reservations, reservationID)
		return ErrReservationNotFound
	}

	loc.OnHand -= res.Qty
	loc.Reserved -= res.Qty

	delete(s.reservations, reservationID)
	return nil
}

func (s *MemoryStore) ExpireDue(ctx context.Context, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for id, res := range s.reservations {
		if res.ExpiresAt.Before(now) {
			locMap, ok := s.levels[res.VariantID]
			if ok {
				loc, ok := locMap[res.LocationID]
				if ok && loc.Reserved >= res.Qty {
					loc.Reserved -= res.Qty
				}
			}
			delete(s.reservations, id)
			count++
		}
	}
	return count, nil
}

func generateID() string {
	return time.Now().Format("20060102150405.999999999")
}
