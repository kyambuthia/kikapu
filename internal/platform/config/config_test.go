package config

import (
	"errors"
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		setup   func()
		cleanup func()
		wantErr error
	}{
		{
			name:    "defaults",
			setup:   func() {},
			cleanup: func() {},
			wantErr: nil,
		},
		{
			name: "custom addr",
			setup: func() {
				os.Setenv("KIKAPU_ADDR", ":9090")
			},
			cleanup: func() {
				os.Unsetenv("KIKAPU_ADDR")
			},
			wantErr: nil,
		},
		{
			name: "migrate mode",
			setup: func() {
				os.Setenv("KIKAPU_MODE", "migrate")
			},
			cleanup: func() {
				os.Unsetenv("KIKAPU_MODE")
			},
			wantErr: nil,
		},
		{
			name: "invalid mode",
			setup: func() {
				os.Setenv("KIKAPU_MODE", "invalid")
			},
			cleanup: func() {
				os.Unsetenv("KIKAPU_MODE")
			},
			wantErr: errors.New("mode must be 'api', 'worker', or 'migrate'"),
		},
		{
			name: "invalid log level",
			setup: func() {
				os.Setenv("KIKAPU_LOG_LEVEL", "invalid")
			},
			cleanup: func() {
				os.Unsetenv("KIKAPU_LOG_LEVEL")
			},
			wantErr: errors.New("invalid log level"),
		},
		{
			name: "paystack provider",
			setup: func() {
				os.Setenv("PAYMENT_PROVIDER", "paystack")
				os.Setenv("PAYSTACK_SECRET_KEY", "sk_test_123")
			},
			cleanup: func() {
				os.Unsetenv("PAYMENT_PROVIDER")
				os.Unsetenv("PAYSTACK_SECRET_KEY")
			},
			wantErr: nil,
		},
		{
			name: "invalid payment provider",
			setup: func() {
				os.Setenv("PAYMENT_PROVIDER", "invalid")
			},
			cleanup: func() {
				os.Unsetenv("PAYMENT_PROVIDER")
			},
			wantErr: errors.New("payment provider must be 'fake', 'paystack', or 'mpesa'"),
		},
		{
			name: "mpesa provider",
			setup: func() {
				os.Setenv("PAYMENT_PROVIDER", "mpesa")
				os.Setenv("MPESA_CONSUMER_KEY", "key")
				os.Setenv("MPESA_CONSUMER_SECRET", "secret")
				os.Setenv("MPESA_SHORT_CODE", "174379")
				os.Setenv("MPESA_PASSKEY", "passkey")
			},
			cleanup: func() {
				os.Unsetenv("PAYMENT_PROVIDER")
				os.Unsetenv("MPESA_CONSUMER_KEY")
				os.Unsetenv("MPESA_CONSUMER_SECRET")
				os.Unsetenv("MPESA_SHORT_CODE")
				os.Unsetenv("MPESA_PASSKEY")
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			defer tt.cleanup()

			cfg, err := Load()
			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("Load() error = nil, want %v", tt.wantErr)
				} else if err.Error() != tt.wantErr.Error() {
					t.Errorf("Load() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("Load() unexpected error = %v", err)
			}
			if cfg == nil {
				t.Error("Load() returned nil config")
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr error
	}{
		{
			name: "valid config",
			config: &Config{
				Addr:            ":8080",
				Mode:            "api",
				LogLevel:        "info",
				PaymentProvider: "fake",
			},
			wantErr: nil,
		},
		{
			name: "empty address",
			config: &Config{
				Addr:            "",
				Mode:            "api",
				LogLevel:        "info",
				PaymentProvider: "fake",
			},
			wantErr: errors.New("address is required"),
		},
		{
			name: "invalid mode",
			config: &Config{
				Addr:            ":8080",
				Mode:            "invalid",
				LogLevel:        "info",
				PaymentProvider: "fake",
			},
			wantErr: errors.New("mode must be 'api', 'worker', or 'migrate'"),
		},
		{
			name: "invalid log level",
			config: &Config{
				Addr:            ":8080",
				Mode:            "api",
				LogLevel:        "invalid",
				PaymentProvider: "fake",
			},
			wantErr: errors.New("invalid log level"),
		},
		{
			name: "worker mode valid",
			config: &Config{
				Addr:            ":8080",
				Mode:            "worker",
				LogLevel:        "debug",
				PaymentProvider: "fake",
			},
			wantErr: nil,
		},
		{
			name: "paystack provider valid",
			config: &Config{
				Addr:            ":8080",
				Mode:            "api",
				LogLevel:        "info",
				PaymentProvider: "paystack",
			},
			wantErr: nil,
		},
		{
			name: "invalid payment provider",
			config: &Config{
				Addr:            ":8080",
				Mode:            "api",
				LogLevel:        "info",
				PaymentProvider: "stripe",
			},
			wantErr: errors.New("payment provider must be 'fake', 'paystack', or 'mpesa'"),
		},
		{
			name: "mpesa provider valid",
			config: &Config{
				Addr:            ":8080",
				Mode:            "api",
				LogLevel:        "info",
				PaymentProvider: "mpesa",
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("Validate() error = nil, want %v", tt.wantErr)
				} else if err.Error() != tt.wantErr.Error() {
					t.Errorf("Validate() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("Validate() unexpected error = %v", err)
			}
		})
	}
}
