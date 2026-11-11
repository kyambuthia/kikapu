package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/kyambuthia/kikapu/internal/platform/httpx"
)

var (
	ErrConflict = errors.New("idempotency: key reused with a different request")
	ErrInFlight = errors.New("idempotency: request with this key is in flight")
	ErrExpired  = errors.New("idempotency: record has expired")
	ErrNotFound = errors.New("idempotency: key not found")
)

type Record struct {
	Key         string
	RequestHash string
	Status      int
	Response    json.RawMessage
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

type Status string

const (
	StatusPending Status = "pending"
	StatusDone    Status = "done"
)

type Store interface {
	Begin(ctx context.Context, key, requestHash string) (Record, bool, error)
	Finish(ctx context.Context, key string, status int, body []byte) error
	Cleanup(ctx context.Context) error
}

type ctxKeyRequestHash struct{}

func SetRequestHash(ctx context.Context, hash string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestHash{}, hash)
}

func GetRequestHash(ctx context.Context) string {
	if h, ok := ctx.Value(ctxKeyRequestHash{}).(string); ok {
		return h
	}
	return ""
}

type ResponseCapture struct {
	http.ResponseWriter
	statusCode int
	body       []byte
}

func (rc *ResponseCapture) WriteHeader(code int) {
	rc.statusCode = code
	rc.ResponseWriter.WriteHeader(code)
}

func (rc *ResponseCapture) Write(b []byte) (int, error) {
	rc.body = append(rc.body, b...)
	return rc.ResponseWriter.Write(b)
}

func Middleware(store Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			if key == "" {
				httpx.WriteError(w, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key header is required", httpx.RequestID(r))
				return
			}

			requestHash := computeRequestHash(r.Method, r.URL.Path, nil)

			rec, replay, err := store.Begin(r.Context(), key, requestHash)
			if err != nil {
				switch err {
				case ErrConflict:
					httpx.WriteError(w, http.StatusConflict, "idempotency_conflict", "This idempotency key was already used with a different request", httpx.RequestID(r))
					return
				case ErrInFlight:
					w.Header().Set("Retry-After", "30")
					httpx.WriteError(w, http.StatusConflict, "request_in_flight", "A request with this idempotency key is already being processed. Retry after the indicated time.", httpx.RequestID(r))
					return
				default:
					httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "Failed to process idempotency key", httpx.RequestID(r))
					return
				}
			}

			if replay {
				w.WriteHeader(rec.Status)
				w.Write(rec.Response)
				return
			}

			ctx := SetRequestHash(r.Context(), requestHash)
			r = r.WithContext(ctx)

			rc := &ResponseCapture{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(rc, r)

			if err := store.Finish(r.Context(), key, rc.statusCode, rc.body); err != nil && err != ErrNotFound {
			}
		})
	}
}

func computeRequestHash(method, path string, body []byte) string {
	return method + ":" + path + ":" + string(body)
}
