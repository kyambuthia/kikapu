package webhook

import (
	"testing"
	"time"
)

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
		t.Error("Same inputs should produce same signature")
	}

	sig3 := Sign("different-secret", ts, body)
	if sig == sig3 {
		t.Error("Different secrets should produce different signatures")
	}
}

func TestBuildSignatureHeader(t *testing.T) {
	ts := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	sig := "abcd1234"

	header := BuildSignatureHeader(ts, sig)

	expected := "t=1704110400,v1=abcd1234"
	if header != expected {
		t.Errorf("Header = %v, want %v", header, expected)
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
			name:     "malformed header",
			header:   "malformed",
			wantErr:  true,
			wantTime: 0,
			wantSig:  "",
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

func TestVerify(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"test":"data"}`)
	ts := time.Now().UTC()
	validSig := Sign(secret, ts, body)
	validHeader := BuildSignatureHeader(ts, validSig)

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
			header:  validHeader,
			body:    body,
			maxAge:  5 * time.Minute,
			wantErr: nil,
		},
		{
			name:    "invalid secret",
			secret:  "wrong-secret",
			header:  validHeader,
			body:    body,
			maxAge:  5 * time.Minute,
			wantErr: ErrInvalidSignature,
		},
		{
			name:    "tampered body",
			secret:  secret,
			header:  validHeader,
			body:    []byte(`{"test":"tampered"}`),
			maxAge:  5 * time.Minute,
			wantErr: ErrInvalidSignature,
		},
		{
			name:    "stale timestamp",
			secret:  secret,
			header:  BuildSignatureHeader(ts.Add(-10*time.Minute), Sign(secret, ts.Add(-10*time.Minute), body)),
			body:    body,
			maxAge:  5 * time.Minute,
			wantErr: ErrStaleTimestamp,
		},
		{
			name:    "empty header",
			secret:  secret,
			header:  "",
			body:    body,
			maxAge:  5 * time.Minute,
			wantErr: ErrMissingSignature,
		},
		{
			name:    "malformed header",
			secret:  secret,
			header:  "malformed",
			body:    body,
			maxAge:  5 * time.Minute,
			wantErr: ErrMalformedSignature,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Verify(tt.secret, tt.header, tt.body, tt.maxAge)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("Verify() error = %v, want %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestVerifyConstantTime(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"test":"data"}`)
	ts := time.Now().UTC()
	validSig := Sign(secret, ts, body)
	validHeader := BuildSignatureHeader(ts, validSig)

	err := Verify("wrong-secret", validHeader, body, 5*time.Minute)
	if err != ErrInvalidSignature {
		t.Errorf("Expected ErrInvalidSignature, got %v", err)
	}
}
