package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestMiddleware_AllowsAndRateLimits(t *testing.T) {
	limiter := NewLimiter(2)
	mw := Middleware(limiter, func(r *http.Request) string { return r.RemoteAddr })
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name       string
		remoteAddr string
		wantStatus int
	}{
		{name: "first key allowed", remoteAddr: "192.0.2.1:1234", wantStatus: http.StatusOK},
		{name: "second key allowed", remoteAddr: "192.0.2.2:1234", wantStatus: http.StatusOK},
		{name: "third key rate limited", remoteAddr: "192.0.2.3:1234", wantStatus: http.StatusTooManyRequests},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/products", nil)
			req.RemoteAddr = tc.remoteAddr
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusTooManyRequests && rec.Header().Get("Retry-After") == "" {
				t.Error("expected Retry-After header on 429 response")
			}
		})
	}
}

func TestMiddlewareWithSizeLimit(t *testing.T) {
	limiter := NewLimiter(10)
	mw := MiddlewareWithSizeLimit(limiter, 1024)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name          string
		contentLength string
		wantStatus    int
	}{
		{name: "within limit", contentLength: "512", wantStatus: http.StatusOK},
		{name: "exceeds limit", contentLength: "2048", wantStatus: http.StatusRequestEntityTooLarge},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/carts", nil)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Length", tc.contentLength)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

func TestMiddlewareKeyIsolationAndConcurrency(t *testing.T) {
	limiter := NewLimiter(1000)
	mw := MiddlewareWithTrustedProxy(limiter, false)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/v1/products", nil)
			req.RemoteAddr = fmt.Sprintf("198.51.100.%d:4321", idx+1)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("concurrent request failed with %d", rec.Code)
			}
		}(i)
	}
	wg.Wait()
}

func TestMiddlewareVariants_AndRequestID(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("MiddlewareWithRate", func(t *testing.T) {
		mw := MiddlewareWithRate(NewLimiter(1), func(r *http.Request) string { return r.RemoteAddr }, 1, time.Second)(okHandler)
		req1 := httptest.NewRequest(http.MethodGet, "/", nil)
		req1.RemoteAddr = "10.0.0.1:1"
		rec1 := httptest.NewRecorder()
		mw.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Fatalf("first status = %d, want 200", rec1.Code)
		}

		req2 := httptest.NewRequest(http.MethodGet, "/", nil)
		req2.RemoteAddr = "10.0.0.2:1"
		rec2 := httptest.NewRecorder()
		mw.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusTooManyRequests {
			t.Fatalf("second status = %d, want 429", rec2.Code)
		}
	})

	t.Run("MiddlewareWithKey", func(t *testing.T) {
		mw := MiddlewareWithKey(NewLimiter(1), "fixed")(okHandler)
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("MiddlewareWithAuth", func(t *testing.T) {
		mw := MiddlewareWithAuth(NewLimiter(1))(okHandler)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer tok-1")
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		reqUnauth := httptest.NewRequest(http.MethodGet, "/", nil)
		recUnauth := httptest.NewRecorder()
		mw.ServeHTTP(recUnauth, reqUnauth)
		if recUnauth.Code != http.StatusTooManyRequests {
			t.Fatalf("second key status = %d, want 429", recUnauth.Code)
		}
	})

	t.Run("LimiterAndTimeout", func(t *testing.T) {
		mw := LimiterAndTimeout(NewLimiter(1), time.Second, okHandler)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(ContextWithRequestID(context.Background(), "req-123"))
		if got := RequestID(req); got != "req-123" {
			t.Fatalf("RequestID = %q, want req-123", got)
		}
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})
}
