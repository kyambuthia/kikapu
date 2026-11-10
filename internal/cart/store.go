package cart

import (
	"sync"
)

type InMemoryStore struct {
	mu    sync.RWMutex
	carts map[string]*Cart
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		carts: make(map[string]*Cart),
	}
}

func (s *InMemoryStore) Get(id string) (*Cart, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cart, ok := s.carts[id]
	if !ok {
		return nil, ErrCartNotFound
	}

	cartCopy := *cart
	linesCopy := make([]Line, len(cart.Lines))
	copy(linesCopy, cart.Lines)
	cartCopy.Lines = linesCopy

	return &cartCopy, nil
}

func (s *InMemoryStore) Save(cart *Cart) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.carts[cart.ID]
	if ok && existing.Version != cart.Version-1 {
		return ErrVersionMismatch
	}

	cartCopy := *cart
	linesCopy := make([]Line, len(cart.Lines))
	copy(linesCopy, cart.Lines)
	cartCopy.Lines = linesCopy

	s.carts[cart.ID] = &cartCopy
	return nil
}
