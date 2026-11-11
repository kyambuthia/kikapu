package mpesa

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kyambuthia/kikapu/internal/payment"
)

type testLogger struct {
	*testing.T
}

func (tl testLogger) Write(p []byte) (n int, err error) {
	tl.Log(string(p))
	return len(p), nil
}

func newTestLogger(t *testing.T) *slog.Logger {
	writer := testLogger{t}
	return slog.New(slog.NewJSONHandler(writer, nil))
}

func TestCallbackHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		callback     CallbackPayload
		existing     *ProcessedCallback
		allowedCIDRs []string
		production   bool
		remoteAddr   string
		wantStatus   int
		wantSaved    bool
		wantErrCode  string
	}{
		{
			name: "success",
			callback: CallbackPayload{
				Body: struct {
					STKCallback STKCallback `json:"stkCallback"`
				}{
					STKCallback: STKCallback{
						MerchantRequestID: "123",
						CheckoutRequestID: "ws_CO_123",
						ResultCode:        0,
						ResultDesc:        "Success",
						CallbackMetadata: &CallbackMetadata{
							Item: []CallbackItem{
								{Name: "Amount", Value: float64(100)},
								{Name: "MpesaReceiptNumber", Value: "QEQ123"},
								{Name: "PhoneNumber", Value: float64(254712345678)},
								{Name: "TransactionDate", Value: float64(20240115103000)},
							},
						},
					},
				},
			},
			wantStatus: http.StatusOK,
			wantSaved:  true,
		},
		{
			name: "duplicate callback",
			callback: CallbackPayload{
				Body: struct {
					STKCallback STKCallback `json:"stkCallback"`
				}{
					STKCallback: STKCallback{
						MerchantRequestID: "123",
						CheckoutRequestID: "ws_CO_123",
						ResultCode:        0,
						ResultDesc:        "Success",
					},
				},
			},
			existing: &ProcessedCallback{
				CheckoutRequestID: "ws_CO_123",
				ResultCode:        0,
				State:             payment.StateCaptured,
			},
			wantStatus: http.StatusOK,
			wantSaved:  false,
		},
		{
			name: "missing metadata",
			callback: CallbackPayload{
				Body: struct {
					STKCallback STKCallback `json:"stkCallback"`
				}{
					STKCallback: STKCallback{
						MerchantRequestID: "123",
						CheckoutRequestID: "ws_CO_123",
						ResultCode:        0,
						ResultDesc:        "Success",
					},
				},
			},
			wantStatus: http.StatusOK,
			wantSaved:  true,
		},
		{
			name: "missing checkout request id",
			callback: CallbackPayload{
				Body: struct {
					STKCallback STKCallback `json:"stkCallback"`
				}{
					STKCallback: STKCallback{
						MerchantRequestID: "123",
						ResultCode:        0,
					},
				},
			},
			wantStatus:  http.StatusBadRequest,
			wantErrCode: "missing_checkout_request_id",
		},
		{
			name: "disallowed source CIDR in production",
			callback: CallbackPayload{
				Body: struct {
					STKCallback STKCallback `json:"stkCallback"`
				}{
					STKCallback: STKCallback{
						CheckoutRequestID: "ws_CO_123",
						ResultCode:        0,
					},
				},
			},
			allowedCIDRs: []string{"192.168.1.0/24"},
			production:   true,
			remoteAddr:   "10.0.0.1:1234",
			wantStatus:   http.StatusForbidden,
			wantErrCode:  "forbidden_source",
		},
		{
			name: "allowed source CIDR",
			callback: CallbackPayload{
				Body: struct {
					STKCallback STKCallback `json:"stkCallback"`
				}{
					STKCallback: STKCallback{
						CheckoutRequestID: "ws_CO_123",
						ResultCode:        0,
						ResultDesc:        "Success",
					},
				},
			},
			allowedCIDRs: []string{"192.168.1.0/24"},
			production:   true,
			remoteAddr:   "192.168.1.100:1234",
			wantStatus:   http.StatusOK,
			wantSaved:    true,
		},
		{
			name: "no allowlist in production fails closed",
			callback: CallbackPayload{
				Body: struct {
					STKCallback STKCallback `json:"stkCallback"`
				}{
					STKCallback: STKCallback{
						CheckoutRequestID: "ws_CO_123",
						ResultCode:        0,
					},
				},
			},
			production:  true,
			wantStatus:  http.StatusForbidden,
			wantErrCode: "ip_allowlist_required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemoryCallbackStore()
			if tt.existing != nil {
				store.SaveCallback(tt.existing)
			}

			logger := newTestLogger(t)

			handler, err := NewCallbackHandler(store, tt.allowedCIDRs, logger, tt.production)
			if err != nil {
				t.Fatalf("failed to create handler: %v", err)
			}

			body, err := json.Marshal(tt.callback)
			if err != nil {
				t.Fatalf("failed to marshal callback: %v", err)
			}

			req := httptest.NewRequest("POST", "/callback", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if tt.remoteAddr != "" {
				req.RemoteAddr = tt.remoteAddr
			}

			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("ServeHTTP() status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantErrCode != "" {
				if !strings.Contains(w.Body.String(), tt.wantErrCode) {
					t.Errorf("ServeHTTP() body = %s, want error code %s", w.Body.String(), tt.wantErrCode)
				}
			}

			if tt.wantSaved {
				cb := tt.callback.Body.STKCallback
				saved, ok := store.GetCallback(cb.CheckoutRequestID, cb.ResultCode)
				if !ok {
					t.Error("callback not saved")
					return
				}
				if saved.CheckoutRequestID != cb.CheckoutRequestID {
					t.Errorf("saved CheckoutRequestID = %s, want %s", saved.CheckoutRequestID, cb.CheckoutRequestID)
				}
			}
		})
	}
}

func TestMapResultCodeFromInt(t *testing.T) {
	tests := []struct {
		code int
		want payment.State
	}{
		{0, payment.StateCaptured},
		{1032, payment.StateFailed},
		{1037, payment.StateFailed},
		{1, payment.StateFailed},
		{999, payment.StateFailed},
	}

	for _, tt := range tests {
		t.Run(map[int]string{0: "0", 1032: "1032", 1037: "1037", 1: "1", 999: "999"}[tt.code], func(t *testing.T) {
			got := mapResultCodeFromInt(tt.code)
			if got != tt.want {
				t.Errorf("mapResultCodeFromInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPAllowed(t *testing.T) {
	tests := []struct {
		name      string
		cidrs     []string
		checkIP   string
		wantAllow bool
	}{
		{
			name:      "match CIDR",
			cidrs:     []string{"192.168.1.0/24"},
			checkIP:   "192.168.1.100",
			wantAllow: true,
		},
		{
			name:      "no match CIDR",
			cidrs:     []string{"192.168.1.0/24"},
			checkIP:   "192.168.2.100",
			wantAllow: false,
		},
		{
			name:      "match single IP",
			cidrs:     []string{"10.0.0.1"},
			checkIP:   "10.0.0.1",
			wantAllow: true,
		},
		{
			name:      "no match single IP",
			cidrs:     []string{"10.0.0.1"},
			checkIP:   "10.0.0.2",
			wantAllow: false,
		},
		{
			name:      "multiple CIDRs",
			cidrs:     []string{"192.168.0.0/16", "10.0.0.0/8"},
			checkIP:   "10.10.10.10",
			wantAllow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewCallbackHandler(NewInMemoryCallbackStore(), tt.cidrs, slog.Default(), false)
			if err != nil {
				t.Fatalf("failed to create handler: %v", err)
			}

			ip := net.ParseIP(tt.checkIP)
			if ip == nil {
				t.Fatalf("invalid IP: %s", tt.checkIP)
			}
			got := handler.ipAllowed(ip)
			if got != tt.wantAllow {
				t.Errorf("ipAllowed() = %v, want %v", got, tt.wantAllow)
			}
		})
	}
}

func TestCallbackPayloadParsing(t *testing.T) {
	jsonPayload := `{
		"Body": {
			"stkCallback": {
				"MerchantRequestID": "12345",
				"CheckoutRequestID": "ws_CO_123456",
				"ResultCode": 0,
				"ResultDesc": "Success",
				"CallbackMetadata": {
					"Item": [
						{"Name": "Amount", "Value": 100},
						{"Name": "MpesaReceiptNumber", "Value": "QEQ123"},
						{"Name": "PhoneNumber", "Value": 254712345678},
						{"Name": "TransactionDate", "Value": 20240115103000}
					]
				}
			}
		}
	}`

	var payload CallbackPayload
	if err := json.Unmarshal([]byte(jsonPayload), &payload); err != nil {
		t.Fatalf("failed to parse payload: %v", err)
	}

	cb := payload.Body.STKCallback
	if cb.MerchantRequestID != "12345" {
		t.Errorf("MerchantRequestID = %s, want 12345", cb.MerchantRequestID)
	}
	if cb.CheckoutRequestID != "ws_CO_123456" {
		t.Errorf("CheckoutRequestID = %s, want ws_CO_123456", cb.CheckoutRequestID)
	}
	if cb.ResultCode != 0 {
		t.Errorf("ResultCode = %d, want 0", cb.ResultCode)
	}
	if cb.CallbackMetadata == nil || len(cb.CallbackMetadata.Item) != 4 {
		t.Errorf("CallbackMetadata items = %d, want 4", len(cb.CallbackMetadata.Item))
	}
}
