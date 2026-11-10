package inventory

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrInsufficientStock   = errors.New("insufficient stock")
	ErrReservationNotFound = errors.New("reservation not found")
	ErrReservationExpired  = errors.New("reservation expired")
)

type Reservation struct {
	ID        string
	VariantID string
	Quantity  int
	ExpiresAt time.Time
	CreatedAt time.Time
}

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now()
}

type FakeClock struct {
	Current time.Time
}

func (f *FakeClock) Now() time.Time {
	return f.Current
}

func (f *FakeClock) Add(d time.Duration) {
	f.Current = f.Current.Add(d)
}

type Service struct {
	mu             sync.RWMutex
	stock          map[string]int
	reservations   map[string]*Reservation
	clock          Clock
	reservationTTL time.Duration
}

func NewService(clock Clock, reservationTTL time.Duration) *Service {
	return &Service{
		stock:          make(map[string]int),
		reservations:   make(map[string]*Reservation),
		clock:          clock,
		reservationTTL: reservationTTL,
	}
}

func (s *Service) SetStock(variantID string, quantity int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stock[variantID] = quantity
}

func (s *Service) Reserve(reservationID, variantID string, quantity int) (*Reservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	available := s.stock[variantID]
	reserved := 0
	for _, r := range s.reservations {
		if r.VariantID == variantID && r.ExpiresAt.After(s.clock.Now()) {
			reserved += r.Quantity
		}
	}

	if available-reserved < quantity {
		return nil, ErrInsufficientStock
	}

	reservation := &Reservation{
		ID:        reservationID,
		VariantID: variantID,
		Quantity:  quantity,
		ExpiresAt: s.clock.Now().Add(s.reservationTTL),
		CreatedAt: s.clock.Now(),
	}

	s.reservations[reservationID] = reservation
	return reservation, nil
}

func (s *Service) Release(reservationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.reservations[reservationID]
	if !ok {
		return ErrReservationNotFound
	}

	delete(s.reservations, reservationID)
	return nil
}

func (s *Service) GetReservation(reservationID string) (*Reservation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	reservation, ok := s.reservations[reservationID]
	if !ok {
		return nil, ErrReservationNotFound
	}

	if reservation.ExpiresAt.Before(s.clock.Now()) {
		return nil, ErrReservationExpired
	}

	return reservation, nil
}

func (s *Service) CleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock.Now()
	for id, r := range s.reservations {
		if r.ExpiresAt.Before(now) {
			delete(s.reservations, id)
		}
	}
}
