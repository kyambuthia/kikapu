package fulfillment

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// InMemoryRepository implements Repository for local development and tests.
type InMemoryRepository struct {
	mu           sync.RWMutex
	fulfillments map[string]*Fulfillment
	byOrder      map[string][]string
}

func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{
		fulfillments: make(map[string]*Fulfillment),
		byOrder:      make(map[string][]string),
	}
}

func (r *InMemoryRepository) Create(_ context.Context, f *Fulfillment) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if f.ID == "" {
		f.ID = "ful_" + uuid.NewString()
	}
	cp := *f
	cp.Items = append([]Item(nil), f.Items...)
	r.fulfillments[cp.ID] = &cp
	r.byOrder[cp.OrderID] = append(r.byOrder[cp.OrderID], cp.ID)
	return nil
}

func (r *InMemoryRepository) Get(_ context.Context, id string) (*Fulfillment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, ok := r.fulfillments[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *f
	cp.Items = append([]Item(nil), f.Items...)
	return &cp, nil
}

func (r *InMemoryRepository) Update(_ context.Context, f *Fulfillment) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.fulfillments[f.ID]; !ok {
		return ErrNotFound
	}
	cp := *f
	cp.Items = append([]Item(nil), f.Items...)
	r.fulfillments[cp.ID] = &cp
	return nil
}

func (r *InMemoryRepository) ListByOrder(_ context.Context, orderID string) ([]Fulfillment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := r.byOrder[orderID]
	out := make([]Fulfillment, 0, len(ids))
	for _, id := range ids {
		if f, ok := r.fulfillments[id]; ok {
			cp := *f
			cp.Items = append([]Item(nil), f.Items...)
			out = append(out, cp)
		}
	}
	return out, nil
}
