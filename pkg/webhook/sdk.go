package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kyambuthia/kikapu/internal/platform/httpx"
)

var (
	ErrMissingSignature   = errors.New("missing signature")
	ErrInvalidSignature   = errors.New("invalid signature")
	ErrStaleTimestamp     = errors.New("stale timestamp")
	ErrMalformedSignature = errors.New("malformed signature")
)

type Handler struct {
	Secret     string
	Tolerance  time.Duration
	Idempotent func(ctx context.Context, id string) (bool, error)
	StoreKey   func(ctx context.Context, id string) error
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.WriteError(w, 400, "invalid_request", "Failed to read body: "+err.Error(), "")
		return
	}
	defer r.Body.Close()

	header := r.Header.Get("X-Kikapu-Signature")
	if err := Verify(h.Secret, header, body, h.Tolerance); err != nil {
		httpx.WriteError(w, 401, "invalid_signature", err.Error(), "")
		return
	}

	deliveryID := r.Header.Get("X-Kikapu-Delivery")

	if h.StoreKey != nil && deliveryID != "" {
		exists, err := h.Idempotent(r.Context(), deliveryID)
		if err != nil {
			httpx.WriteError(w, 500, "idempotent_error", "Failed to check idempotency", "")
			return
		}
		if exists {
			w.WriteHeader(200)
			return
		}

		if err := h.StoreKey(r.Context(), deliveryID); err != nil {
			httpx.WriteError(w, 500, "store_error", "Failed to store receipt", "")
			return
		}
	}

	w.WriteHeader(200)
}

func Verify(secret string, header string, body []byte, maxAge time.Duration) error {
	if header == "" {
		return ErrMissingSignature
	}

	ts, sig, err := ParseSignatureHeader(header)
	if err != nil {
		return err
	}

	if time.Since(ts) > maxAge {
		return ErrStaleTimestamp
	}

	expectedSig := Sign(secret, ts, body)

	if subtle.ConstantTimeCompare([]byte(sig), []byte(expectedSig)) != 1 {
		return ErrInvalidSignature
	}

	return nil
}

func Sign(secret string, ts time.Time, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts.Unix(), 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func ParseSignatureHeader(header string) (time.Time, string, error) {
	parts := strings.Split(header, ",")
	if len(parts) < 2 {
		return time.Time{}, "", fmt.Errorf("%w: malformed header - need 't=' and 'v1=' components", ErrMalformedSignature)
	}

	var timestamp int64
	var signature string

	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return time.Time{}, "", fmt.Errorf("%w: malformed header '%s'", ErrMalformedSignature, part)
		}

		switch kv[0] {
		case "t":
			parsed, err := strconv.ParseInt(kv[1], 10, 64)
			if err != nil {
				return time.Time{}, "", fmt.Errorf("%w: invalid timestamp '%s'", ErrInvalidSignature, kv[1])
			}
			timestamp = parsed
		case "v1":
			signature = kv[1]
		}
	}

	if timestamp == 0 || signature == "" {
		return time.Time{}, "", fmt.Errorf("%w: missing timestamp or signature in header", ErrMissingSignature)
	}

	return time.Unix(timestamp, 0), signature, nil
}

func BuildSignatureHeader(ts time.Time, signature string) string {
	return fmt.Sprintf("t=%d,v1=%s", ts.Unix(), signature)
}

type InMemoryIdempotentStore struct {
	deliveryIDs map[string]bool
}

func NewInMemoryIdempotentStore() *InMemoryIdempotentStore {
	return &InMemoryIdempotentStore{
		deliveryIDs: make(map[string]bool),
	}
}

func (s *InMemoryIdempotentStore) Exists(ctx context.Context, id string) (bool, error) {
	return s.deliveryIDs[id], nil
}

func (s *InMemoryIdempotentStore) Save(ctx context.Context, id string) error {
	s.deliveryIDs[id] = true
	return nil
}

var _ http.Handler = (*Handler)(nil)
