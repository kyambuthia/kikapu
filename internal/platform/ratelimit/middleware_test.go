package ratelimit

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestMiddleware429(t *testing.T) {
	tests := []struct {
		name             string
		rateLimit        int
		requestCount     int
		expect2xx        bool
		expectRetryAfter bool
	}{
		{
			name:             "first request allowed",
			rateLimit:        10,
			requestCount:     1,
			expect2xx:        true,
			expectRetryAfter: false,
		},
		{
			name:             "requests within limit allowed",
			rateLimit:        10,
			requestCount:     5,
			expect2xx:        true,
			expectRetryAfter: false,
		},
		{
			name:             "request exceeds limit",
			rateLimit:        2,
			requestCount:     3,
			expect2xx:        false,
			expectRetryAfter: true,
		},
		{
			name:             "exactly at limit",
			rateLimit:        2,
			requestCount:     2,
			expect2xx:        true,
			expectRetryAfter: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := NewLimiter(tt.rateLimit * 2) // Larger size to prevent eviction
			nextCalled := false
			nextToUse := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				http.Error(w, "ok", http.StatusOK)
			})

			middleman := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reqID := RequestID(r)
				if reqID == "" {
					t.Error("request ID should be present")
				}
				w.Header().Set("X-Request-ID", reqID)
				nextToUse.ServeHTTP(w, r)
			})

			middlewareThenMiddleman := LimiterAndTimeout(limiter, time.Second, middleman)

			for i := 0; i < tt.requestCount; i++ {
				req := httptest.NewRequest("GET", "/", nil)
				w := httptest.NewRecorder()
				middlewareThenMiddleman.ServeHTTP(w, req)

				resp := w.Result()
				if i == tt.requestCount-1 && !nextCalled {
					t.Errorf("next handler was not called")
				}
				if i == tt.requestCount-1 {
					if resp.StatusCode != http.StatusOK && tt.expect2xx {
						t.Errorf("request %d should be allowed", i)
					}
					if resp.StatusCode != http.StatusTooManyRequests && !tt.expect2xx {
						t.Errorf("request %d should be 429", i)
					}
					retryAfter := w.Header().Get("Retry-After")
					if resp.StatusCode == http.StatusTooManyRequests && tt.expectRetryAfter && retryAfter == "" {
						t.Errorf("request %d should have Retry-After header", i)
					}
				}
				if i == tt.requestCount-1 {
					resp.Body.Close()
				}
			}
		})
	}
}

func TestMiddlewareRateLimitCheckout(t *testing.T) {
	limiter := NewLimiter(200)
	nextCalled := false
	nextToUse := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		http.Error(w, "ok", http.StatusOK)
	})

	middleman := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := RequestID(r)
		if reqID == "" {
			t.Error("request ID should be present")
		}
		w.Header().Set("X-Request-ID", reqID)
		nextToUse.ServeHTTP(w, r)
	})

	middlewareWithTimeout := LimiterAndTimeout(limiter, time.Minute, middleman)

	req := httptest.NewRequest("POST", "/v1/checkout", nil)
	w := httptest.NewRecorder()

	for i := 0; i < 15; i++ {
		middlewareWithTimeout.ServeHTTP(w, req)
		resp := w.Result()
		if i < 10 {
			if resp.StatusCode != http.StatusOK {
				t.Errorf("request %d should be allowed for checkout", i)
			}
		} else {
			if resp.StatusCode != http.StatusTooManyRequests {
				t.Errorf("request %d should be 429 for checkout", i)
			}
		}
		resp.Body.Close()
	}
}

func TestMiddlewareRateLimitAdmin(t *testing.T) {
	limiter := NewLimiter(200)
	nextCalled := false
	nextToUse := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		http.Error(w, "ok", http.StatusOK)
	})

	middleman := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := RequestID(r)
		if reqID == "" {
			t.Error("request ID should be present")
		}
		w.Header().Set("X-Request-ID", reqID)
		nextToUse.ServeHTTP(w, r)
	})

	middlewareWithTimeout := LimiterAndTimeout(limiter, time.Minute, middleman)

	req := httptest.NewRequest("GET", "/v1/admin/products", nil)
	w := httptest.NewRecorder()

	for i := 0; i < 10; i++ {
		middlewareWithTimeout.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d should be allowed for admin", i)
		}
	}
}

func TestMiddlewareRateLimitPublicRead(t *testing.T) {
	limiter := NewLimiter(200)
	nextCalled := false
	nextToUse := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		http.Error(w, "ok", http.StatusOK)
	})

	middleman := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := RequestID(r)
		if reqID == "" {
			t.Error("request ID should be present")
		}
		w.Header().Set("X-Request-ID", reqID)
		nextToUse.ServeHTTP(w, r)
	})

	middlewareWithTimeout := LimiterAndTimeout(limiter, time.Minute, middleman)

	req := httptest.NewRequest("GET", "/v1/products", nil)
	w := httptest.NewRecorder()

	for i := 0; i < 30; i++ {
		middlewareWithTimeout.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d should be allowed for public read", i)
		}
	}
}

func TestMiddleware413Oversized(t *testing.T) {
	limiter := NewLimiter(1000)
	nextCalled := false
	nextToUse := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		http.Error(w, "ok", http.StatusOK)
	})

	middleman := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := RequestID(r)
		if reqID == "" {
			t.Error("request ID should be present")
		}
		w.Header().Set("X-Request-ID", reqID)
		nextToUse.ServeHTTP(w, r)
	})

	middlewareWithTimeout := LimiterAndTimeout(limiter, time.Second, middleman)

	req := httptest.NewRequest("POST", "/v1/checkout", nil)
	req.Header.Set("Content-Length", "2048")
	req.Body = http.MaxBytesReader(w, req.Body, 512)
	w := httptest.NewRecorder()

	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if reqSizeExceeds(r, 1024) {
				http.Error(w, "Request entity too large", http.StatusRequestEntityTooLarge)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	middlewareWithTimeout.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("request should have 413 status code, got %d", w.Code)
	}
}

func TestMiddlewareXForwardedForSpoofing(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		expectedIP string
	}{
		{
			name: "no X-Forwarded-For",
			headers: map[string]string{
				"X-Client-IP": "192.168.1.1",
			},
			expectedIP: "192.168.1.1",
		},
		{
			name: "X-Forwarded-For present",
			headers: map[string]string{
				"X-Forwarded-For": "10.0.0.1, 192.168.1.1",
				"X-Client-IP":     "192.168.1.1",
			},
			expectedIP: "192.168.1.1",
		},
		{
			name: "X-Forwarded-For strict check",
			headers: map[string]string{
				"X-Forwarded-For": "10.0.0.1, 192.168.1.1, 172.16.0.1",
				"X-Client-IP":     "192.168.1.1",
			},
			expectedIP: "172.16.0.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := NewLimiter(1000)
			nextCalled := false
			nextToUse := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				http.Error(w, "ok", http.StatusOK)
			})

			middleman := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reqID := RequestID(r)
				if reqID == "" {
					t.Error("request ID should be present")
				}
				w.Header().Set("X-Request-ID", reqID)
				nextToUse.ServeHTTP(w, r)
			})

			middlewareWithTimeout := LimiterAndTimeout(limiter, time.Second, middleman)
			middleware := MiddlewareWithTrustedProxy(limiter, false)

			req := httptest.NewRequest("GET", "/", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			middlewareWithTimeout.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Logf("Expected 200 OK, got %d", w.Code)
			}
			if !nextCalled {
				t.Error("next handler should have been called")
			}
		})
	}
}

func TestMiddlewareKeyIsolation(t *testing.T) {
	limiter1 := NewLimiter(1000)
	limiter2 := NewLimiter(1000)

	next1 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "ok", http.StatusOK)
	})
	next2 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "ok", http.StatusOK)
	})

	middleware1 := MiddlewareWithKey(limiter1, "key1")
	middleware2 := MiddlewareWithKey(limiter2, "key2")

	req := httptest.NewRequest("GET", "/", nil)
	w1 := httptest.NewRecorder()
	w2 := httptest.NewRecorder()

	middleware1(w1, req)
	middleware2(w2, req)

	if w1.Code != http.StatusOK || w2.Code != http.StatusOK {
		t.Error("both requests should succeed with different keys")
	}
}

func TestMiddlewareConcurrentRequests(t *testing.T) {
	limiter := NewLimiter(1000)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "ok", http.StatusOK)
	})
	middleware := Middleware(limiter, "client_ip")

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()
			middleware(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("concurrent request failed with code %d", w.Code)
			}
		}()
	}
	wg.Wait()
}

func TestMiddlewareWindowRefill(t *testing.T) {
	limiter := NewLimiter(200)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "ok", http.StatusOK)
	})
	middleware := MiddlewareWithRate(limiter, "api_key", 5, time.Minute)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	for i := 0; i < 5; i++ {
		req = httptest.NewRequest("GET", "/", nil)
		w = httptest.NewRecorder()
		middleware(w, req)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("request %d should be allowed", i)
		}
	}

	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-Time", time.Now().Add(time.Minute+5).Format(time.RFC3339))
	w = httptest.NewRecorder()
	middleware(w, req)
	if w.Code != http.StatusOK {
		t.Error("should allow after window refill")
	}
}

func TestMiddlewareRetryAfter(t *testing.T) {
	limiter := NewLimiter(1)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "ok", http.StatusOK)
	})
	middleware := MiddlewareWithRate(limiter, "api_key", 1, time.Second)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	for i := 0; i < 2; i++ {
		middleware(w, req)
		if i == 0 {
			if resp.StatusCode != http.StatusOK {
				t.Errorf("first request should be allowed")
			}
		} else {
			if resp.StatusCode != http.StatusTooManyRequests {
				t.Errorf("second request should be 429")
			}
			retryAfter := w.Header().Get("Retry-After")
			if retryAfter == "" {
				t.Error("second request should have Retry-After header")
			}
			t.Logf("Retry-After header: %s", retryAfter)
		}
		w = httptest.NewRecorder()
	}
}

func TestMiddlewareAPIKeyDetection(t *testing.T) {
	tests := []struct {
		name        string
		headers     map[string]string
		expectedKey string
	}{
		{
			name: "Authorization header with Bearer token",
			headers: map[string]string{
				"Authorization": "Bearer kik_abc123",
			},
			expectedKey: "Bearer kik_abc123",
		},
		{
			name: "X-API-Key header",
			headers: map[string]string{
				"X-API-Key": "key123",
			},
			expectedKey: "key123",
		},
		{
			name: "Default to client IP when no auth",
			headers: map[string]string{
				"X-Client-IP": "192.168.1.1",
			},
			expectedKey: "192.168.1.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := NewLimiter(1000)
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "ok", http.StatusOK)
			})
			middleware := MiddlewareWithAuth(limiter)

			req := httptest.NewRequest("GET", "/", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			middleware(w, req)

			if w.Code != http.StatusOK {
				t.Logf("Expected 200 OK, got %d", w.Code)
			}
		})
	}
}

func TestMiddlewareAPIRequestTracing(t *testing.T) {
	limiter := NewLimiter(1000)
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		runtimeID := ctxRuntimeID(r.Context())
		if runtimeID == "" {
			t.Error("runtime ID should be set in context")
		}
	})

	middleman := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := RequestID(r.Context())
		if reqID == "" {
			t.Error("request ID should be present")
		}
		w.Header().Set("X-Request-ID", reqID)
		next.ServeHTTP(w, r)
	})

	middleware := LimiterAndTimeout(limiter, time.Second, middleman)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	middleware(w, req)

	if !nextCalled {
		t.Error("next handler should be called")
	}
}

func TestMiddlewareBurstThenDeny(t *testing.T) {
	limiter := NewLimiter(200)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "ok", http.StatusOK)
	})
	middle := LimiterAndTimeout(limiter, time.Second, next)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	for i := 0; i < 5; i++ {
		req = httptest.NewRequest("GET", "/", nil)
		w = httptest.NewRecorder()
		middle.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d should be allowed (burst)", i)
		}
	}

	req = httptest.NewRequest("GET", "/", nil)
	w = httptest.NewRecorder()
	middle.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Error("should deny after burst")
	}
}

func TestMiddlewareRequestIDs(t *testing.T) {
	limiter := NewLimiter(1000)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "ok", http.StatusOK)
	})
	middleware := LimiterAndTimeout(limiter, time.Second, next)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "test-request-id")
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	requestID := w.Header().Get("X-Request-ID")
	if requestID != "test-request-id" {
		t.Errorf("expected 'test-request-id', got '%s'", requestID)
	}
}

type nopCloser struct {
	*bytes.Buffer
}

func (nopCloser) Close() error {
	return nil
}

func TestMiddlewareWithTrustedProxyTrue(t *testing.T) {
	limiter := NewLimiter(1000)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "ok", http.StatusOK)
	})
	middleware := MiddlewareWithTrustedProxy(limiter, true)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Logf("Expected 200 OK with trusted proxy, got %d", w.Code)
	}
}

func TestMiddlewareWithTrustedProxyFalse(t *testing.T) {
	limiter := NewLimiter(1000)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "ok", http.StatusOK)
	})
	middleware := MiddlewareWithTrustedProxy(limiter, false)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Logf("Expected 200 OK without trusted proxy, got %d", w.Code)
	}
}
