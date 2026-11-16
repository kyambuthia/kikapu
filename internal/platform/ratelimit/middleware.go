package ratelimit

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/kyambuthia/kikapu/internal/platform/httpx"
)

func Middleware(limiter *Limiter, keyFunc func(r *http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFunc(r)
			if !limiter.Allow(key, time.Now()) {
				w.Header().Set("Retry-After", "1")
				_ = httpx.WriteError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "rate limit exceeded", httpx.RequestID(r))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func MiddlewareWithRate(limiter *Limiter, keyFunc func(r *http.Request) string, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow(keyFunc(r), time.Now()) {
				w.Header().Set("Retry-After", "1")
				_ = httpx.WriteError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "rate limit exceeded", httpx.RequestID(r))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func MiddlewareWithSizeLimit(limiter *Limiter, maxSize int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if reqSizeExceeds(r, maxSize) {
				http.Error(w, "Request entity too large", http.StatusRequestEntityTooLarge)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func reqSizeExceeds(r *http.Request, maxSize int) bool {
	contentLength := r.Header.Get("Content-Length")
	if contentLength != "" {
		var size int
		if _, err := fmt.Sscanf(contentLength, "%d", &size); err == nil && size <= maxSize {
			return false
		}
		return true
	}
	return false
}

// MiddlewareWithTrustedProxy rate-limits requests by client IP.
// X-Forwarded-For is only consulted when trusted is true; otherwise
// r.RemoteAddr is used so callers cannot spoof headers to bypass limits.
func MiddlewareWithTrustedProxy(limiter *Limiter, trusted bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer := clientIP(r, trusted)
			if !limiter.Allow(peer, time.Now()) {
				w.Header().Set("Retry-After", "1")
				_ = httpx.WriteError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "rate limit exceeded", httpx.RequestID(r))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func MiddlewareWithKey(limiter *Limiter, key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow(key, time.Now()) {
				w.Header().Set("Retry-After", "1")
				_ = httpx.WriteError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "rate limit exceeded", httpx.RequestID(r))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func MiddlewareWithAuth(limiter *Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			var key string
			if auth != "" {
				key = auth
			} else {
				key = r.Header.Get("X-API-Key")
				if key == "" {
					key = RemoteAddr(r)
				}
			}
			if !limiter.Allow(key, time.Now()) {
				w.Header().Set("Retry-After", "1")
				_ = httpx.WriteError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "rate limit exceeded", httpx.RequestID(r))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func LimiterAndTimeout(limiter *Limiter, timeout time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		req := r.WithContext(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			next.ServeHTTP(w, req)
		}()

		select {
		case <-done:
			return
		case <-ctx.Done():
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Request timeout", http.StatusRequestTimeout)
		}
	})
}

func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, &requestIDKey{}, requestID)
}

type requestIDKey struct{}

func ctxRuntimeID(ctx context.Context) string {
	if id, ok := ctx.Value(&requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

func RequestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return id
	}
	if ctxID, ok := r.Context().Value(&requestIDKey{}).(string); ok {
		return ctxID
	}
	return ""
}

func generateRequestID() string {
	var buf bytes.Buffer
	buf.Grow(16)
	for i := 0; i < 16; i++ {
		b := byte('a' + byte(i%26))
		buf.WriteByte(b)
	}
	return buf.String()
}

func RemoteAddr(r *http.Request) string {
	return r.RemoteAddr
}

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			if first, _, _ := strings.Cut(forwarded, ","); strings.TrimSpace(first) != "" {
				return strings.TrimSpace(first)
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}
