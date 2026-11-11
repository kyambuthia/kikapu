package obs

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

type ctxKeyRoutePattern struct{}

type responseWriterMetrics struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (rw *responseWriterMetrics) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriterMetrics) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		rw := &responseWriterMetrics{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(rw, r)

		duration := time.Since(start).Seconds()
		route := GetRoutePattern(r)
		status := strconv.Itoa(rw.statusCode)

		RequestsTotal.WithLabelValues(route, r.Method, status).Inc()
		RequestDuration.WithLabelValues(route).Observe(duration)
	})
}

func SetRoutePattern(r *http.Request, pattern string) {
	ctx := context.WithValue(r.Context(), ctxKeyRoutePattern{}, pattern)
	*r = *r.WithContext(ctx)
}

func GetRoutePattern(r *http.Request) string {
	if pattern, ok := r.Context().Value(ctxKeyRoutePattern{}).(string); ok && pattern != "" {
		return pattern
	}
	return r.URL.Path
}

func InstrumentCheckout(result string) {
	CheckoutTotal.WithLabelValues(result).Inc()
}
