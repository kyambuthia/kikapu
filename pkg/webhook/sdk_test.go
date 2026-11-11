package webhook

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kyambuthia/kikapu/internal/webhook"
)

func TestVerify(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"test":"data"}`)
	now := time.Now()
	fiveMinutes := 5 * time.Minute

	tests := []struct {
		name    string
		secret  string
		header  string
		body    []byte
		maxAge  time.Duration
		wantErr error
	}{
		{
			name:    "valid signature",
			secret:  secret,
			header:  BuildSignatureHeader(now, Sign(secret, now, body)),
			body:    body,
			maxAge:  5 * time.Minute,
			wantErr: nil,
		},
		{
			name:    "invalid secret",
			secret:  "wrong-secret",
			header:  BuildSignatureHeader(now, Sign(secret, now, body)),
			body:    body,
			maxAge:  5 * time.Minute,
			wantErr: ErrInvalidSignature,
		},
		{
			name:    "tampered body",
			secret:  secret,
			header:  BuildSignatureHeader(now, Sign(secret, now, body)),
			body:    []byte(`{"test":"tampered"}`),
			maxAge:  5 * time.Minute,
			wantErr: ErrInvalidSignature,
		},
		{
			name:    "stale timestamp",
			secret:  secret,
			header:  BuildSignatureHeader(now.Add(-6*time.Minute), Sign(secret, now.Add(-6*time.Minute), body)),
			body:    body,
			maxAge:  fiveMinutes,
			wantErr: ErrStaleTimestamp,
		},
		{
			name:    "fresh timestamp slightly old",
			secret:  secret,
			header:  BuildSignatureHeader(now.Add(-1*time.Minute), Sign(secret, now.Add(-1*time.Minute), body)),
			body:    body,
			maxAge:  fiveMinutes,
			wantErr: nil,
		},
		{
			name:    "empty header",
			secret:  secret,
			header:  "",
			body:    body,
			maxAge:  fiveMinutes,
			wantErr: webhook.ErrMissingSignature,
		},
		{
			name:    "header with no parts",
			secret:  secret,
			header:  " ",
			body:    body,
			maxAge:  fiveMinutes,
			wantErr: webhook.ErrMalformedSignature,
		},
		{
			name:    "invalid timestamp value",
			secret:  secret,
			header:  "t=invalid,v1=fake",
			body:    body,
			maxAge:  fiveMinutes,
			wantErr: errors.New("invalid signature"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Verify(tt.secret, tt.header, tt.body, tt.maxAge)
			if tt.wantErr != nil && err != tt.wantErr {
				t.Errorf("Verify() error = %v, want %v", err, tt.wantErr)
				return
			}
			if tt.wantErr == nil && err != nil {
				t.Errorf("Verify() unexpected error: %v", err)
			}
		})
	}
}

func TestSign(t *testing.T) {
	secret := "test-secret"
	ts := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	body := []byte(`{"test":"data"}`)

	sig := Sign(secret, ts, body)

	if sig == "" {
		t.Error("Signature should not be empty")
	}

	sig2 := Sign(secret, ts, body)
	if sig != sig2 {
		t.Fatal("Same inputs should produce same signature")
	}

	sig3 := Sign("different-secret", ts, body)
	if sig == sig3 {
		t.Fatal("Different secrets should produce different signatures")
	}
}

func TestBuildSignatureHeader(t *testing.T) {
	ts := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	header := BuildSignatureHeader(ts, "abcd1234")

	expected := "t=1704110400,v1=abcd1234"
	if header != expected {
		t.Errorf("BuildSignatureHeader() = %v, want %v", header, expected)
	}
}

func TestParseSignatureHeader(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		wantErr  bool
		wantTime int64
		wantSig  string
	}{
		{
			name:     "valid header",
			header:   "t=1704110400,v1=abcd1234",
			wantErr:  false,
			wantTime: 1704110400,
			wantSig:  "abcd1234",
		},
		{
			name:     "empty header",
			header:   "",
			wantErr:  true,
			wantTime: 0,
			wantSig:  "",
		},
		{
			name:     "missing timestamp",
			header:   "v1=abcd1234",
			wantErr:  true,
			wantTime: 0,
			wantSig:  "",
		},
		{
			name:     "missing signature",
			header:   "t=1704110400",
			wantErr:  true,
			wantTime: 0,
			wantSig:  "",
		},
		{
			name:     "invalid timestamp",
			header:   "t=invalid,v1=abcd1234",
			wantErr:  true,
			wantTime: 0,
			wantSig:  "",
		},
		{
			name:     "malformed header without equals",
			header:   "t=1704110400v1=abcd1234",
			wantErr:  true,
			wantTime: 0,
			wantSig:  "",
		},
		{
			name:     "header with extra fields",
			header:   "t=1704110400,v1=abcd1234,custom=value",
			wantErr:  false,
			wantTime: 1704110400,
			wantSig:  "abcd1234",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, sig, err := ParseSignatureHeader(tt.header)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if ts.Unix() != tt.wantTime {
				t.Errorf("Timestamp = %v, want %v", ts.Unix(), tt.wantTime)
			}
			if sig != tt.wantSig {
				t.Errorf("Signature = %v, want %v", sig, tt.wantSig)
			}
		})
	}
}

func TestHandlerServeHTTP(t *testing.T) {
	secret := "test-secret"
	handler := Handler{
		Secret:     secret,
		Tolerance:  5 * time.Minute,
		Idempotent: func(ctx context.Context, id string) (bool, error) { return false, nil },
	}

	tests := []struct {
		name       string
		body       []byte
		sigHeader  string
		deliveryID string
		event      string
		wantStatus int
	}{
		{
			name:       "missing signature - 401",
			body:       []byte(`{"test":"data"}`),
			sigHeader:  "",
			deliveryID: "delivery_001",
			event:      "order.paid",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid signature - 401",
			body:       []byte(`{"test":"data"}`),
			sigHeader:  "t=" + strconv.FormatInt(time.Now().Unix(), 10) + ",v1=fake",
			deliveryID: "delivery_001",
			event:      "order.paid",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "stale timestamp - 401",
			body:       []byte(`{"test":"data"}`),
			sigHeader:  "t=1577836800,v1=fake",
			deliveryID: "delivery_001",
			event:      "order.paid",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "missing delivery ID - dedupe skipped - 200 OK",
			body:       []byte(`{"test":"data"}`),
			sigHeader:  "t=" + strconv.FormatInt(time.Now().Unix(), 10) + ",v1=fake_sig",
			deliveryID: "",
			event:      "order.paid",
			wantStatus: http.StatusOK,
		},
		{
			name:       "idempotent key returns true - 200 OK (skip processing)",
			body:       []byte(`{"test":"data"}`),
			sigHeader:  "t=" + strconv.FormatInt(time.Now().Unix(), 10) + ",v1=fake",
			deliveryID: "already_processed",
			event:      "order.paid",
			wantStatus: http.StatusOK,
		},
		{
			name:       "store key fails - 500 error",
			body:       []byte(`{"test":"data"}`),
			sigHeader:  "t=" + strconv.FormatInt(time.Now().Unix(), 10) + ",v1=fake",
			deliveryID: "should_fail",
			event:      "order.paid",
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/webhook", nil)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Kikapu-Signature", tt.sigHeader)
			r.Header.Set("X-Kikapu-Delivery", tt.deliveryID)
			r.Header.Set("X-Kikapu-Event", tt.event)
			r.Body = io.NopCloser(strings.NewReader(string(tt.body)))

			w := httptest.NewRecorder()

			if tt.name == "store key fails on valid signature" {
				handler.StoreKey = func(ctx context.Context, id string) error {
					return fmt.Errorf("storage failure")
				}
			} else if tt.name == "idempotent key returns true" {
				handler.Idempotent = func(ctx context.Context, id string) (bool, error) {
					return true, nil
				}
			}

			handler.ServeHTTP(w, r)

			if w.Code != tt.wantStatus {
				t.Errorf("Handler.ServeHTTP() status = %v, want %v", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestInMemoryIdempotentStore(t *testing.T) {
	store := NewInMemoryIdempotentStore()
	ctx := context.Background()

	t.Run("save and retrieve", func(t *testing.T) {
		if err := store.Save(ctx, "id_001"); err != nil {
			t.Errorf("Save() error = %v", err)
		}

		exists, err := store.Exists(ctx, "id_001")
		if err != nil {
			t.Errorf("Exists() error = %v", err)
		}
		if !exists {
			t.Error("Exists() should return true for saved ID")
		}
	})

	t.Run("non-existent key", func(t *testing.T) {
		exists, err := store.Exists(ctx, "nonexistent")
		if err != nil {
			t.Errorf("Exists() error = %v", err)
		}
		if exists {
			t.Error("Exists() should return false for non-existent key")
		}
	})

	t.Run("full lifecycle", func(t *testing.T) {
		store.Save(ctx, "id_full")
		exists1, _ := store.Exists(ctx, "id_full")
		store.Save(ctx, "id_full")
		exists2, _ := store.Exists(ctx, "id_full")

		if !exists1 || !exists2 {
			t.Error("Both checks should return true")
		}
	})
}

func TestHandlerIntegration(t *testing.T) {
	secret := "test-secret"
	handler := Handler{
		Secret:    secret,
		Tolerance: 5 * time.Minute,
	}

	t.Run("happy path - full flow", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/webhook", nil)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Kikapu-Signature", "t="+strconv.FormatInt(time.Now().Unix(), 10)+",v1=fake_sig")
		r.Header.Set("X-Kikapu-Delivery", "test_delivery")
		r.Header.Set("X-Kikapu-Event", "order.paid")
		r.Body = io.NopCloser(strings.NewReader(`{"test":"data"}`))

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		// With fake signature, we expect 401
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected 401, got %d", w.Code)
		}
	})

	t.Run("idempotent handler flow", func(t *testing.T) {
		store := NewInMemoryIdempotentStore()

		idempotentHandler := Handler{
			Secret:    secret,
			Tolerance: 5 * time.Minute,
			Idempotent: func(ctx context.Context, id string) (bool, error) {
				return store.Exists(ctx, id)
			},
			StoreKey: func(ctx context.Context, id string) error {
				return nil
			},
		}

		ctx := context.Background()

		// First request - processes and saves
		r1 := httptest.NewRequest("POST", "/webhook", nil)
		r1.Header.Set("Content-Type", "application/json")
		r1.Header.Set("X-Kikapu-Signature", "t="+strconv.FormatInt(time.Now().Unix(), 10)+",v1=fake_sig")
		r1.Header.Set("X-Kikapu-Delivery", "first_delivery")
		r1.Header.Set("X-Kikapu-Event", "order.paid")
		r1.Body = io.NopCloser(strings.NewReader(`{"test":1}`))

		w1 := httptest.NewRecorder()
		idempotentHandler.ServeHTTP(w1, r1)

		// Second request with same delivery ID - should be skipped
		r2 := httptest.NewRequest("POST", "/webhook", nil)
		r2.Header.Set("Content-Type", "application/json")
		r2.Header.Set("X-Kikapu-Signature", "t="+strconv.FormatInt(time.Now().Unix(), 10)+",v1=fake_sig")
		r2.Header.Set("X-Kikapu-Delivery", "first_delivery")
		r2.Header.Set("X-Kikapu-Event", "order.paid")
		r2.Body = io.NopCloser(strings.NewReader(`{"test":2}`))

		w2 := httptest.NewRecorder()
		idempotentHandler.ServeHTTP(w2, r2)

		if w1.Code != http.StatusOK {
			t.Errorf("First request failed: %d", w1.Code)
		}
		if w2.Code != http.StatusOK {
			t.Errorf("Second request failed: %d", w2.Code)
		}

		exists, _ := store.Exists(ctx, "first_delivery")
		if !exists {
			t.Error("Delivery ID should be in store")
		}
	})
}

func TestEdgeCases(t *testing.T) {
	secret := "test-secret"
	now := time.Now()
	tenMinutes := 10 * time.Minute

	t.Run("very old timestamp", func(t *testing.T) {
		header := "t=1577836800,v1=fake_sig"

		err := Verify(secret, header, []byte("{}"), tenMinutes)
		if err != ErrStaleTimestamp {
			t.Errorf("Expected stale timestamp error, got: %v", err)
		}
	})

	t.Run("future timestamp", func(t *testing.T) {
		futureTime := now.Add(24 * time.Hour)
		header := "t=" + strconv.FormatInt(futureTime.Unix(), 10) + ",v1=fake_sig"

		err := Verify(secret, header, []byte("{}"), tenMinutes)
		if err != nil {
			t.Errorf("Expected no error for future timestamp, got: %v", err)
		}
	})

	t.Run("large body", func(t *testing.T) {
		now := time.Now()
		largeBody := make([]byte, 1024*1024) // 1MB
		sig := Sign(secret, now, largeBody)
		header := BuildSignatureHeader(now, sig)

		err := Verify(secret, header, largeBody, 5*time.Minute)
		if err != nil {
			t.Errorf("Expected no error with large body, got: %v", err)
		}
	})

	t.Run("unicode characters in body", func(t *testing.T) {
		now := time.Now()
		unicodeBody := []byte(`{"test": "🌍🚀"}`)
		sig := Sign(secret, now, unicodeBody)
		header := BuildSignatureHeader(now, sig)

		err := Verify(secret, header, unicodeBody, 5*time.Minute)
		if err != nil {
			t.Errorf("Expected no error with unicode, got: %v", err)
		}
	})
}
