package region

import (
	"errors"
	"sync"
)

var ErrRegionNotFound = errors.New("region: region not found")

type InMemoryRepository struct {
	mu      sync.RWMutex
	regions map[string]*Region
}

func NewInMemoryRepository(regions []Region) *InMemoryRepository {
	m := make(map[string]*Region)
	for i := range regions {
		m[regions[i].ID] = &regions[i]
	}
	return &InMemoryRepository{regions: m}
}

func (r *InMemoryRepository) Get(ctx interface{}, id string) (*Region, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	reg, ok := r.regions[id]
	if !ok {
		return nil, ErrRegionNotFound
	}
	return reg, nil
}

func (r *InMemoryRepository) List(ctx interface{}) ([]Region, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Region, 0, len(r.regions))
	for _, reg := range r.regions {
		result = append(result, *reg)
	}
	return result, nil
}
