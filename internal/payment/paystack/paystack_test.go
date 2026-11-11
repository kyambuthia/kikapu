package paystack

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/kyambuthia/kikapu/internal/payment"
	"github.com/kyambuthia/kikapu/internal/platform/money"
)

const testSecretKey = "sk_test_dummy_key"

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				SecretKey: testSecretKey,
			},
			wantErr: false,
		},
		{
			name: "valid config with custom base url",
			config: Config{
				SecretKey: testSecretKey,
				BaseURL:   "https://custom.paystack.co",
			},
			wantErr: false,
		},
		{
			name: "missing secret key",
			config: Config{
				SecretKey: "",
			},
			wantErr: true,
		},
		{
			name: "invalid base url",
			config: Config{
				SecretKey: testSecretKey,
				BaseURL:   "://invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, err := New(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && client == nil {
				t.Error("New() returned nil client")
			}
		})
	}
}

func TestCharge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErr    bool
		wantStatus payment.State
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+testSecretKey {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}

				resp := initializeResponse{
					Status:  true,
					Message: "Authorization URL created",
					Data: struct {
						AuthorizationURL string `json:"authorization_url"`
						AccessCode       string `json:"access_code"`
						Reference        string `json:"reference"`
					}{
						AuthorizationURL: "https://checkout.paystack.com/test",
						AccessCode:       "ACCESS_CODE",
						Reference:        "REF_123",
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			},
			wantErr:    false,
			wantStatus: payment.StatePending,
		},
		{
			name: "non-2xx response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				io.WriteString(w, "internal error")
			},
			wantErr: true,
		},
		{
			name: "timeout",
			handler: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(2 * time.Second)
				w.WriteHeader(http.StatusOK)
			},
			wantErr: true,
		},
		{
			name: "invalid json response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, "invalid json")
			},
			wantErr: true,
		},
		{
			name: "failed request",
			handler: func(w http.ResponseWriter, r *http.Request) {
				resp := initializeResponse{
					Status:  false,
					Message: "Invalid request",
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client, err := New(Config{
				BaseURL:   server.URL,
				SecretKey: testSecretKey,
				HTTPClient: &http.Client{
					Timeout: 1 * time.Second,
				},
			})
			if err != nil {
				t.Fatalf("Failed to create client: %v", err)
			}

			result, err := client.Charge(payment.ChargeRequest{
				IDempotencyKey: "test_key",
				Amount:         money.Amount{Value: 10000, Currency: "KES"},
				CustomerID:     "test@example.com",
			})

			if (err != nil) != tt.wantErr {
				t.Errorf("Charge() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if result.Status != tt.wantStatus {
					t.Errorf("Charge() status = %v, want %v", result.Status, tt.wantStatus)
				}
			}
		})
	}
}

func TestVerify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		reference  string
		handler    http.HandlerFunc
		wantErr    bool
		wantStatus payment.State
	}{
		{
			name:      "success",
			reference: "REF_123",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+testSecretKey {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}

				resp := verifyResponse{
					Status:  true,
					Message: "Verification successful",
					Data: struct {
						ID            int64  `json:"id"`
						Status        string `json:"status"`
						Reference     string `json:"reference"`
						Amount        int64  `json:"amount"`
						Currency      string `json:"currency"`
						PaidAt        string `json:"paid_at"`
						CreatedAt     string `json:"created_at"`
						Channel       string `json:"channel"`
						Authorization struct {
							AuthorizationCode string `json:"authorization_code"`
						} `json:"authorization"`
					}{
						ID:        12345,
						Status:    "success",
						Reference: "REF_123",
						Amount:    10000,
						Currency:  "KES",
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			},
			wantErr:    false,
			wantStatus: payment.StateCaptured,
		},
		{
			name:      "failed status",
			reference: "REF_456",
			handler: func(w http.ResponseWriter, r *http.Request) {
				resp := verifyResponse{
					Status:  true,
					Message: "Verification successful",
					Data: struct {
						ID            int64  `json:"id"`
						Status        string `json:"status"`
						Reference     string `json:"reference"`
						Amount        int64  `json:"amount"`
						Currency      string `json:"currency"`
						PaidAt        string `json:"paid_at"`
						CreatedAt     string `json:"created_at"`
						Channel       string `json:"channel"`
						Authorization struct {
							AuthorizationCode string `json:"authorization_code"`
						} `json:"authorization"`
					}{
						ID:        12346,
						Status:    "failed",
						Reference: "REF_456",
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			},
			wantErr:    false,
			wantStatus: payment.StateFailed,
		},
		{
			name:      "abandoned status",
			reference: "REF_789",
			handler: func(w http.ResponseWriter, r *http.Request) {
				resp := verifyResponse{
					Status:  true,
					Message: "Verification successful",
					Data: struct {
						ID            int64  `json:"id"`
						Status        string `json:"status"`
						Reference     string `json:"reference"`
						Amount        int64  `json:"amount"`
						Currency      string `json:"currency"`
						PaidAt        string `json:"paid_at"`
						CreatedAt     string `json:"created_at"`
						Channel       string `json:"channel"`
						Authorization struct {
							AuthorizationCode string `json:"authorization_code"`
						} `json:"authorization"`
					}{
						ID:        12347,
						Status:    "abandoned",
						Reference: "REF_789",
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			},
			wantErr:    false,
			wantStatus: payment.StatePending,
		},
		{
			name:      "unauthorized",
			reference: "REF_000",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
			wantErr: true,
		},
		{
			name:      "non-2xx",
			reference: "REF_001",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client, err := New(Config{
				BaseURL:   server.URL,
				SecretKey: testSecretKey,
				HTTPClient: &http.Client{
					Timeout: 1 * time.Second,
				},
			})
			if err != nil {
				t.Fatalf("Failed to create client: %v", err)
			}

			result, err := client.Verify(tt.reference)

			if (err != nil) != tt.wantErr {
				t.Errorf("Verify() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && result.Status != tt.wantStatus {
				t.Errorf("Verify() status = %v, want %v", result.Status, tt.wantStatus)
			}
		})
	}
}

func TestWebhookSignature(t *testing.T) {
	t.Parallel()

	body := []byte(`{"event":"charge.success","data":{"id":12345,"reference":"REF_123","status":"success","amount":10000,"currency":"KES"}}`)

	validSig := computeSignature(body, testSecretKey)

	tests := []struct {
		name      string
		signature string
		body      []byte
		wantCode  int
	}{
		{
			name:      "valid signature",
			signature: validSig,
			body:      body,
			wantCode:  http.StatusOK,
		},
		{
			name:      "tampered body",
			signature: validSig,
			body:      []byte(`{"event":"charge.success","data":{"id":12345}}`),
			wantCode:  http.StatusUnauthorized,
		},
		{
			name:      "missing signature",
			signature: "",
			body:      body,
			wantCode:  http.StatusUnauthorized,
		},
		{
			name:      "invalid signature",
			signature: "invalid",
			body:      body,
			wantCode:  http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.NotFoundHandler())
			defer server.Close()

			client, err := New(Config{
				BaseURL:   server.URL,
				SecretKey: testSecretKey,
			})
			if err != nil {
				t.Fatalf("Failed to create client: %v", err)
			}

			eventStore := NewInMemoryEventStore()
			logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
			handler := NewWebhookHandler(client, eventStore, logger)

			req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			if tt.signature != "" {
				req.Header.Set("X-Paystack-Signature", tt.signature)
			}
			req.Header.Set("X-Request-ID", "test-request-id")

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("ServeHTTP() status = %v, want %v", w.Code, tt.wantCode)
			}
		})
	}
}

func TestWebhookIdempotency(t *testing.T) {
	t.Parallel()

	body := `{"event":"charge.success","data":{"id":12345,"reference":"REF_123","status":"success","amount":10000,"currency":"KES"}}`

	client, err := New(Config{
		SecretKey: testSecretKey,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	eventStore := NewInMemoryEventStore()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	handler := NewWebhookHandler(client, eventStore, logger)

	sig := computeSignature([]byte(body), testSecretKey)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Paystack-Signature", sig)
		req.Header.Set("X-Request-ID", "test-request-"+strconv.Itoa(i))

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: expected status 200, got %d", i+1, w.Code)
		}
	}

	event, ok := eventStore.GetEvent("12345")
	if !ok {
		t.Error("Event not saved in event store")
	} else if event.Reference != "REF_123" {
		t.Errorf("Expected reference REF_123, got %s", event.Reference)
	}
}

func TestWebhookUnknownEventType(t *testing.T) {
	t.Parallel()

	body := `{"event":"unknown.event","data":{"id":12345,"reference":"REF_123"}}`

	client, err := New(Config{
		SecretKey: testSecretKey,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	eventStore := NewInMemoryEventStore()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	handler := NewWebhookHandler(client, eventStore, logger)

	sig := computeSignature([]byte(body), testSecretKey)

	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Paystack-Signature", sig)
	req.Header.Set("X-Request-ID", "test-request")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 for unknown event type, got %d", w.Code)
	}
}

func computeSignature(body []byte, key string) string {
	h := hmac.New(sha512.New, []byte(key))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}
