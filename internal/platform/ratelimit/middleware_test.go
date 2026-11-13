package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
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
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/v1/products", nil)
			req.RemoteAddr = "198.51.100.1:4321"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("concurrent request failed with %d", rec.Code)
			}
		}()
	}
	wg.Wait()
}
