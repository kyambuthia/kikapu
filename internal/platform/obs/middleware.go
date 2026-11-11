package obs

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type ctxKeyRoutePattern struct{}

func SetRoutePattern(r *http.Request, pattern string) *http.Request {
	return r.WithContext(SetRoutePatternToContext(r.Context(), pattern))
}

func SetRoutePatternToContext(ctx context.Context, pattern string) context.Context {
	return context.WithValue(ctx, ctxKeyRoutePattern{}, pattern)
}

func GetRoutePattern(r *http.Request) string {
	if pattern, ok := r.Context().Value(ctxKeyRoutePattern{}).(string); ok {
		return pattern
	}
	return r.URL.Path
}

func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)

		duration := time.Since(start).Seconds()
		route := GetRoutePattern(r)
		method := r.Method
		status := strconv.Itoa(rw.statusCode)

		RequestsTotal.WithLabelValues(route, method, status).Inc()
		RequestDuration.WithLabelValues(route).Observe(duration)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func ExtractRoutePattern(path string) string {
	if path == "/" {
		return "/"
	}

	parts := strings.Split(path, "/")
	var result []string
	for _, part := range parts {
		if part == "" {
			continue
		}
		result = append(result, part)
	}

	if len(result) == 0 {
		return "/"
	}

	for i, part := range result {
		if isUUID(part) || isNumeric(part) || isSlug(part) {
			result[i] = "{id}"
		}
	}

	return "/" + strings.Join(result, "/")
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
		} else {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

func isNumeric(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

func isSlug(s string) bool {
	if len(s) < 4 {
		return false
	}
	hasHyphen := false
	for _, r := range s {
		if r == '-' {
			hasHyphen = true
			continue
		}
		if r == '_' {
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return hasHyphen
}
