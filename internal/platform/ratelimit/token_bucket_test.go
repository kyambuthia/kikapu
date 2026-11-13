package ratelimit

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestBucketRefill(t *testing.T) {
	bucket := &Bucket{
		tokens:     1,
		lastRefill: time.Now().Add(-2 * time.Second),
	}
	bucket.Refill(time.Now())
	if bucket.tokens != refillTokens {
		t.Errorf("expected %d tokens after refill, got %d", refillTokens, bucket.tokens)
	}
}

func TestBucketAllow(t *testing.T) {
	tests := []struct {
		name           string
		initialTokens  uint64
		refillDuration time.Duration
		requestCount   int
	}{
		{
			name:           "allow with available token",
			initialTokens:  1,
			refillDuration: time.Second,
			requestCount:   1,
		},
		{
			name:           "refill before next request",
			initialTokens:  1,
			refillDuration: time.Second,
			requestCount:   2,
		},
		{
			name:           "deny after exhausting tokens",
			initialTokens:  2,
			refillDuration: time.Second,
			requestCount:   3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			bucket := &Bucket{
				tokens:     tt.initialTokens,
				lastRefill: now,
			}
			for i := 0; i < tt.requestCount; i++ {
				bucket.Refill(now)
				allowed := bucket.Allow()
				if tt.requestCount > int(tt.initialTokens) && i == tt.requestCount-1 && allowed {
					t.Errorf("expected deny on last request")
				}
				if tt.requestCount <= int(tt.initialTokens) && !allowed {
					t.Errorf("expected allow on request %d", i)
				}
			}
		})
	}
}

func TestLimiterAllow(t *testing.T) {
	now := time.Now()
	limiter := NewLimiter(10)
	t.Run("first request allowed", func(t *testing.T) {
		allowed := limiter.Allow("key1", now)
		if !allowed {
			t.Error("expected first request to be allowed")
		}
		if len(limiter.buckets) != 1 {
			t.Errorf("expected 1 bucket, got %d", len(limiter.buckets))
		}
	})
	t.Run("consecutive requests allowed", func(t *testing.T) {
		for i := 0; i < 10; i++ {
			allowed := limiter.Allow("key1", now.Add(time.Duration(i+1)*time.Second))
			if !allowed {
				t.Errorf("request %d should be allowed", i)
			}
		}
		if len(limiter.buckets) != 1 {
			t.Errorf("expected 1 bucket for same key, got %d", len(limiter.buckets))
		}
	})
	t.Run("different key allows separate requests", func(t *testing.T) {
		allowed := limiter.Allow("key2", now)
		if !allowed {
			t.Error("expected request to different key to be allowed")
		}
		if len(limiter.buckets) != 2 {
			t.Errorf("expected 2 buckets, got %d", len(limiter.buckets))
		}
	})
}

func TestLimiterCapacity(t *testing.T) {
	now := time.Now()
	limiter := NewLimiter(3)
	t.Run("limit reached prevents new keys", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			limiter.Allow("key1", now)
		}
		allowed := limiter.Allow("key2", now)
		if !allowed {
			t.Error("should allow when at capacity (LIFO eviction)")
		}
	})
	t.Run("eviction prevents new keys", func(t *testing.T) {
		capLimiter := NewLimiter(10)
		for i := 0; i < 10; i++ {
			capLimiter.Allow(fmt.Sprintf("key-%d", i), now)
		}
		capLimiter.Evict("key-0")
		if len(capLimiter.buckets) != 9 {
			t.Errorf("expected 9 buckets after eviction, got %d", len(capLimiter.buckets))
		}
	})
}

func TestLimiterRefill(t *testing.T) {
	now := time.Now()
	bucket := &Bucket{
		tokens:     1,
		lastRefill: now.Add(-1 * time.Second),
	}
	bucket.Refill(now.Add(5 * time.Second))
	if bucket.tokens != refillTokens {
		t.Errorf("expected %d tokens after refill, got %d", refillTokens, bucket.tokens)
	}
	if !bucket.Allow() {
		t.Error("expected allow after refill")
	}
}

func TestLimiterAllowConcurrent(t *testing.T) {
	now := time.Now()
	limiter := NewLimiter(1000)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", idx)
			if !limiter.Allow(key, now) {
				t.Errorf("concurrent request %d failed", idx)
			}
		}(i)
	}
	wg.Wait()
}

func TestLimiterKeyIsolation(t *testing.T) {
	now := time.Now()
	limiter := NewLimiter(10)
	key1Requests := 10
	key2Requests := 10
	for i := 0; i < key1Requests; i++ {
		limiter.Allow("key1", now)
	}
	for i := 0; i < key2Requests; i++ {
		limiter.Allow("key2", now)
	}
	if len(limiter.buckets) != 2 {
		t.Errorf("expected 2 isolated buckets, got %d", len(limiter.buckets))
	}
}
