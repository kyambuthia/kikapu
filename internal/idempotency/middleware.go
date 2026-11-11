package idempotency

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/kyambuthia/kikapu/internal/platform/httpx"
)

type ctxKeyRecord struct{}

func Middleware(store Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			if key == "" {
				httpx.WriteError(w, http.StatusBadRequest,
					"missing_idempotency_key",
					"Idempotency-Key header is required",
					httpx.RequestID(r))
				return
			}

			body, err := io.ReadAll(r.Body)
			if err != nil {
				httpx.WriteError(w, http.StatusBadRequest,
					"invalid_request_body",
					"Failed to read request body",
					httpx.RequestID(r))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			requestHash := HashRequest(r.Method, r.URL.Path, body)

			rec, replay, err := store.Begin(r.Context(), key, requestHash)
			if err != nil {
				switch err {
				case ErrConflict:
					httpx.WriteError(w, http.StatusConflict,
						"idempotency_conflict",
						"Idempotency key reused with a different request",
						httpx.RequestID(r))
					return
				case ErrInFlight:
					httpx.WriteError(w, http.StatusConflict,
						"request_in_flight",
						"Request with this idempotency key is currently being processed",
						httpx.RequestID(r))
					return
				case ErrExpired:
				default:
					httpx.WriteError(w, http.StatusInternalServerError,
						"idempotency_error",
						"Failed to process idempotency key",
						httpx.RequestID(r))
					return
				}
			}

			if replay {
				w.Header().Set("X-Idempotency-Replayed", "true")
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(rec.Status)
				w.Write(rec.Response)
				return
			}

			recByte, _ := json.Marshal(Record{
				Key:         key,
				RequestHash: requestHash,
			})
			ctx := context.WithValue(r.Context(), ctxKeyRecord{}, recByte)
			r = r.WithContext(ctx)

			rw := &realResponseRecorder{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				body:           &bytes.Buffer{},
			}

			next.ServeHTTP(rw, r)

			store.Finish(r.Context(), key, rw.statusCode, rw.body.Bytes())
		})
	}
}

type realResponseRecorder struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
}

func (rw *realResponseRecorder) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *realResponseRecorder) Write(b []byte) (int, error) {
	rw.body.Write(b)
	return rw.ResponseWriter.Write(b)
}

func (rw *realResponseRecorder) Header() http.Header {
	return rw.ResponseWriter.Header()
}

func GetRecord(ctx context.Context) *Record {
	rec, ok := ctx.Value(ctxKeyRecord{}).([]byte)
	if !ok {
		return nil
	}
	var record Record
	if err := json.Unmarshal(rec, &record); err != nil {
		return nil
	}
	return &record
}
