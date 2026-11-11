package channel

import (
	"errors"
	"sync"
)

var ErrChannelNotFound = errors.New("channel: channel not found")

type InMemoryRepository struct {
	mu       sync.RWMutex
	channels map[string]*SalesChannel
	bySlug   map[string]*SalesChannel
}

func NewInMemoryRepository(channels []SalesChannel) *InMemoryRepository {
	m := make(map[string]*SalesChannel)
	bySlug := make(map[string]*SalesChannel)
	for i := range channels {
		ch := &channels[i]
		m[ch.ID] = ch
		bySlug[ch.Slug] = ch
	}
	return &InMemoryRepository{channels: m, bySlug: bySlug}
}

func (r *InMemoryRepository) Get(ctx interface{}, id string) (*SalesChannel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ch, ok := r.channels[id]
	if !ok {
		return nil, ErrChannelNotFound
	}
	return ch, nil
}

func (r *InMemoryRepository) GetBySlug(ctx interface{}, slug string) (*SalesChannel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ch, ok := r.bySlug[slug]
	if !ok {
		return nil, ErrChannelNotFound
	}
	return ch, nil
}

func (r *InMemoryRepository) List(ctx interface{}) ([]SalesChannel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]SalesChannel, 0, len(r.channels))
	for _, ch := range r.channels {
		result = append(result, *ch)
	}
	return result, nil
}
