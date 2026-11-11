package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"
)

type ctxKeyRequestID struct{}

type ErrorEnvelope struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func WriteJSON(w http.ResponseWriter, status int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return nil
	}
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(data)
}

func WriteError(w http.ResponseWriter, status int, code, message, requestID string) error {
	envelope := ErrorEnvelope{
		Error: ErrorDetail{
			Code:      code,
			Message:   message,
			RequestID: requestID,
		},
	}
	return WriteJSON(w, status, envelope)
}

func RequestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return id
	}
	if ctxID, ok := r.Context().Value(ctxKeyRequestID{}).(string); ok {
		return ctxID
	}
	return ""
}

func SetRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestID{}, id)
}

func generateRequestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = generateRequestID()
		}
		ctx := SetRequestID(r.Context(), requestID)
		r = r.WithContext(ctx)
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r)
	})
}

type LoggingMiddlewareConfig struct {
	GetRoute func(*http.Request) string
	GetError func(*http.Request) string
}

func LoggingMiddlewareWithConfig(logger interface{ Info(string, ...any) }, cfg LoggingMiddlewareConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rw, r)
			duration := time.Since(start)
			requestID := RequestID(r)

			route := r.URL.Path
			if cfg.GetRoute != nil {
				if pattern := cfg.GetRoute(r); pattern != "" {
					route = pattern
				}
			}

			errorCode := ""
			if cfg.GetError != nil {
				errorCode = cfg.GetError(r)
			}

			args := []interface{}{
				"request_id", requestID,
				"route", route,
				"method", r.Method,
				"status", rw.statusCode,
				"duration_ms", duration.Milliseconds(),
			}
			if errorCode != "" {
				args = append(args, "error_code", errorCode)
			}

			logger.Info("request", args...)
		})
	}
}

func LoggingMiddleware(logger interface{ Info(string, ...any) }) func(http.Handler) http.Handler {
	return LoggingMiddlewareWithConfig(logger, LoggingMiddlewareConfig{})
}
