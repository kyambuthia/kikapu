package ratelimit

import (
	"sync"
	"time"
)

type Bucket struct {
	tokens     uint64
	lastRefill time.Time
}

const refillTokens = 1

func (b *Bucket) Refill(now time.Time) {
	if now.After(b.lastRefill) {
		b.tokens = refillTokens
		b.lastRefill = now
	}
}

func (b *Bucket) Allow() bool {
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*Bucket
	size    int
}

func NewLimiter(size int) *Limiter {
	return &Limiter{
		buckets: make(map[string]*Bucket),
		size:    size,
	}
}

func (l *Limiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	bucket, exists := l.buckets[key]
	if !exists {
		if len(l.buckets) >= l.size {
			return false
		}
		bucket = &Bucket{
			tokens:     refillTokens,
			lastRefill: now,
		}
		l.buckets[key] = bucket
		return true
	}
	bucket.Refill(now)
	return bucket.Allow()
}

func (l *Limiter) Evict(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}
