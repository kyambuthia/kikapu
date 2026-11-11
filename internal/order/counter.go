package order

import (
	"fmt"
	"sync"
)

type InMemoryCounter struct {
	mu       sync.Mutex
	counters map[string]int64
}

func NewInMemoryCounter() *InMemoryCounter {
	return &InMemoryCounter{
		counters: make(map[string]int64),
	}
}

func (c *InMemoryCounter) Next(ctx interface{}, regionID string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.counters[regionID]++
	return c.counters[regionID], nil
}

func (c *InMemoryCounter) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counters = make(map[string]int64)
}

func FormatOrderNumber(seq int64) string {
	return fmt.Sprintf("KIK-%06d", seq)
}
