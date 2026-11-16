package ratelimit

import (
	"sync"
	"time"
)

const (
	refillTokens   = 1
	refillInterval = time.Second
	staleBucketTTL = 5 * time.Minute
)

type Bucket struct {
	tokens     uint64
	lastRefill time.Time
}

// Refill replenishes tokens only after at least one refillInterval has elapsed
// since the last refill, preventing sub-second bursts from refilling every call.
func (b *Bucket) Refill(now time.Time) {
	if now.Sub(b.lastRefill) >= refillInterval {
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
			l.evictStaleLocked(now)
		}
		if len(l.buckets) >= l.size {
			return false
		}
		// Consume one token for the initial request.
		bucket = &Bucket{
			tokens:     refillTokens - 1,
			lastRefill: now,
		}
		l.buckets[key] = bucket
		return true
	}
	bucket.Refill(now)
	return bucket.Allow()
}

func (l *Limiter) evictStaleLocked(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.lastRefill) >= staleBucketTTL {
			delete(l.buckets, k)
		}
	}
}

func (l *Limiter) Evict(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}
