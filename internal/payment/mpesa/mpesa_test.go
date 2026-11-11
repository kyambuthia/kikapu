package mpesa

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kyambuthia/kikapu/internal/payment"
	"github.com/kyambuthia/kikapu/internal/platform/money"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr error
	}{
		{
			name: "valid config",
			config: Config{
				ConsumerKey:    "key",
				ConsumerSecret: "secret",
				ShortCode:      "174379",
				Passkey:        "passkey",
			},
			wantErr: nil,
		},
		{
			name: "missing consumer key",
			config: Config{
				ConsumerSecret: "secret",
				ShortCode:      "174379",
				Passkey:        "passkey",
			},
			wantErr: ErrInvalidConfig,
		},
		{
			name: "missing consumer secret",
			config: Config{
				ConsumerKey: "key",
				ShortCode:   "174379",
				Passkey:     "passkey",
			},
			wantErr: ErrInvalidConfig,
		},
		{
			name: "missing short code",
			config: Config{
				ConsumerKey:    "key",
				ConsumerSecret: "secret",
				Passkey:        "passkey",
			},
			wantErr: ErrInvalidConfig,
		},
		{
			name: "missing passkey",
			config: Config{
				ConsumerKey:    "key",
				ConsumerSecret: "secret",
				ShortCode:      "174379",
			},
			wantErr: ErrInvalidConfig,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.config)
			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("New() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("New() unexpected error: %v", err)
			}
		})
	}
}

func TestOAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupServer  func(w http.ResponseWriter, r *http.Request)
		wantErr      bool
		wantToken    string
		reuseToken   bool
		refreshAfter string
	}{
		{
			name: "success",
			setupServer: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/oauth/v1/generate" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"access_token":"test_token","expires_in":3600}`))
			},
			wantErr:   false,
			wantToken: "test_token",
		},
		{
			name: "cached token reuse",
			setupServer: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/oauth/v1/generate" {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"access_token":"test_token","expires_in":3600}`))
					return
				}
				auth := r.Header.Get("Authorization")
				if !strings.HasPrefix(auth, "Bearer ") {
					t.Errorf("missing Bearer authorization")
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"ResponseCode":"0","CheckoutRequestID":"ws_CO_123"}`))
			},
			reuseToken: true,
			wantToken:  "test_token",
		},
		{
			name: "refresh after 401",
			setupServer: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/oauth/v1/generate" {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"access_token":"refreshed_token","expires_in":3600}`))
					return
				}
				auth := r.Header.Get("Authorization")
				if strings.Contains(auth, "Bearer old_token") {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"ResponseCode":"0","CheckoutRequestID":"test123"}`))
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.setupServer))
			defer server.Close()

			client, err := New(Config{
				BaseURL:        server.URL,
				ConsumerKey:    "key",
				ConsumerSecret: "secret",
				ShortCode:      "174379",
				Passkey:        "passkey",
				HTTPClient:     server.Client(),
			})
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			token, err := client.getAccessToken(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Errorf("getAccessToken() error = nil, wantErr %v", tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("getAccessToken() unexpected error: %v", err)
				return
			}

			if tt.wantToken != "" && token != tt.wantToken {
				t.Errorf("getAccessToken() token = %v, want %v", token, tt.wantToken)
			}

			if tt.reuseToken {
				token2, err := client.getAccessToken(context.Background())
				if err != nil {
					t.Errorf("getAccessToken() second call error: %v", err)
					return
				}
				if token2 != token {
					t.Errorf("getAccessToken() token not reused: %v != %v", token2, token)
				}
			}
		})
	}
}

func TestCharge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setupServer func(w http.ResponseWriter, r *http.Request, callCount *int)
		phone       string
		wantErr     bool
		wantStatus  payment.State
	}{
		{
			name: "success",
			setupServer: func(w http.ResponseWriter, r *http.Request, callCount *int) {
				*callCount++
				if r.URL.Path == "/oauth/v1/generate" {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"access_token":"test_token","expires_in":3600}`))
					return
				}
				if r.Method != "POST" {
					t.Errorf("unexpected method: %s", r.Method)
					return
				}
				body, _ := io.ReadAll(r.Body)
				var req stkPushRequest
				if err := json.Unmarshal(body, &req); err != nil {
					t.Errorf("failed to parse request: %v", err)
					return
				}
				if req.Amount != 100 {
					t.Errorf("unexpected amount: %d", req.Amount)
				}
				if req.PhoneNumber != "254712345678" {
					t.Errorf("unexpected phone: %s", req.PhoneNumber)
				}
				decoded, _ := base64.StdEncoding.DecodeString(req.Password)
				passwordParts := strings.Split(string(decoded), "")
				if len(passwordParts) < 3 || !strings.HasPrefix(string(decoded), "174379passkey") {
					t.Errorf("unexpected password format: %s", string(decoded))
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"ResponseCode":"0","CheckoutRequestID":"ws_CO_123456","MerchantRequestID":"123"}`))
			},
			phone:      "254712345678",
			wantErr:    false,
			wantStatus: payment.StatePending,
		},
		{
			name: "phone normalization 07XX",
			setupServer: func(w http.ResponseWriter, r *http.Request, callCount *int) {
				*callCount++
				if r.URL.Path == "/oauth/v1/generate" {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"access_token":"test_token","expires_in":3600}`))
					return
				}
				body, _ := io.ReadAll(r.Body)
				var req stkPushRequest
				json.Unmarshal(body, &req)
				if req.PhoneNumber != "254712345678" {
					t.Errorf("phone not normalized: %s", req.PhoneNumber)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"ResponseCode":"0","CheckoutRequestID":"ws_CO_123456"}`))
			},
			phone:      "0712345678",
			wantErr:    false,
			wantStatus: payment.StatePending,
		},
		{
			name: "phone normalization +254",
			setupServer: func(w http.ResponseWriter, r *http.Request, callCount *int) {
				*callCount++
				if r.URL.Path == "/oauth/v1/generate" {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"access_token":"test_token","expires_in":3600}`))
					return
				}
				body, _ := io.ReadAll(r.Body)
				var req stkPushRequest
				json.Unmarshal(body, &req)
				if req.PhoneNumber != "254712345678" {
					t.Errorf("phone not normalized: %s", req.PhoneNumber)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"ResponseCode":"0","CheckoutRequestID":"ws_CO_123456"}`))
			},
			phone:      "+254712345678",
			wantErr:    false,
			wantStatus: payment.StatePending,
		},
		{
			name: "non-2xx response",
			setupServer: func(w http.ResponseWriter, r *http.Request, callCount *int) {
				*callCount++
				if r.URL.Path == "/oauth/v1/generate" {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"access_token":"test_token","expires_in":3600}`))
					return
				}
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"errorCode":"400","errorMessage":"Bad Request"}`))
			},
			phone:   "254712345678",
			wantErr: true,
		},
		{
			name: "non-zero response code",
			setupServer: func(w http.ResponseWriter, r *http.Request, callCount *int) {
				*callCount++
				if r.URL.Path == "/oauth/v1/generate" {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"access_token":"test_token","expires_in":3600}`))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"ResponseCode":"1","ResponseDescription":"Failed"}`))
			},
			phone:   "254712345678",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var callCount int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tt.setupServer(w, r, &callCount)
			}))
			defer server.Close()

			client, err := New(Config{
				BaseURL:        server.URL,
				ConsumerKey:    "key",
				ConsumerSecret: "secret",
				ShortCode:      "174379",
				Passkey:        "passkey",
				HTTPClient:     server.Client(),
			})
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			req := payment.ChargeRequest{
				IDempotencyKey: "key123",
				Amount:         money.NewAmount(100, "KES"),
				CustomerID:     tt.phone,
			}

			result, err := client.Charge(req)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Charge() error = nil, wantErr %v", tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("Charge() unexpected error: %v", err)
				return
			}

			if result.Status != tt.wantStatus {
				t.Errorf("Charge() status = %v, want %v", result.Status, tt.wantStatus)
			}
		})
	}
}

func TestVerify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		resultCode string
		wantState  payment.State
	}{
		{
			name:       "success code 0",
			resultCode: "0",
			wantState:  payment.StateCaptured,
		},
		{
			name:       "cancelled code 1032",
			resultCode: "1032",
			wantState:  payment.StateFailed,
		},
		{
			name:       "timeout code 1037",
			resultCode: "1037",
			wantState:  payment.StateFailed,
		},
		{
			name:       "unknown code",
			resultCode: "999",
			wantState:  payment.StateFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/oauth/v1/generate" {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"access_token":"test_token","expires_in":3600}`))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(fmt.Sprintf(`{"ResponseCode":"0","ResultCode":"%s","ResultDesc":"OK"}`, tt.resultCode)))
			}))
			defer server.Close()

			client, err := New(Config{
				BaseURL:        server.URL,
				ConsumerKey:    "key",
				ConsumerSecret: "secret",
				ShortCode:      "174379",
				Passkey:        "passkey",
				HTTPClient:     server.Client(),
			})
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			result, err := client.Verify("ws_CO_123456")
			if err != nil {
				t.Errorf("Verify() unexpected error: %v", err)
				return
			}

			if result.Status != tt.wantState {
				t.Errorf("Verify() status = %v, want %v", result.Status, tt.wantState)
			}
		})
	}
}

func TestGeneratePassword(t *testing.T) {
	shortCode := "174379"
	passkey := "passkey"
	timestamp := "20240115103000"
	expectedPassword := generatePassword(shortCode, passkey, timestamp)

	if expectedPassword == "" {
		t.Error("generatePassword() returned empty string")
	}

	decoded, err := base64.StdEncoding.DecodeString(expectedPassword)
	if err != nil {
		t.Errorf("generatePassword() returned invalid base64: %v", err)
		return
	}

	decodedStr := string(decoded)
	expected := shortCode + passkey + timestamp
	if decodedStr != expected {
		t.Errorf("generatePassword() decoded = %s, want %s", decodedStr, expected)
	}
}

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		phone string
		want  string
	}{
		{"0712345678", "254712345678"},
		{"+254712345678", "254712345678"},
		{"254712345678", "254712345678"},
		{" 254712345678 ", "254712345678"},
	}

	for _, tt := range tests {
		t.Run(tt.phone, func(t *testing.T) {
			got := normalizePhone(tt.phone)
			if got != tt.want {
				t.Errorf("normalizePhone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatTimestamp(t *testing.T) {
	tests := []struct {
		name string
		time time.Time
		want string
	}{
		{
			name: "known time",
			time: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			want: "20240115103000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTimestamp(tt.time)
			if got != tt.want {
				t.Errorf("formatTimestamp() = %v, want %v", got, tt.want)
			}
		})
	}
}
