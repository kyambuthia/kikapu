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
			name: "invalid mode",
			setup: func() {
				os.Setenv("KIKAPU_MODE", "invalid")
			},
			cleanup: func() {
				os.Unsetenv("KIKAPU_MODE")
			},
			wantErr: errors.New("mode must be 'api' or 'worker'"),
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
				Addr:     ":8080",
				Mode:     "api",
				LogLevel: "info",
			},
			wantErr: nil,
		},
		{
			name: "empty address",
			config: &Config{
				Addr:     "",
				Mode:     "api",
				LogLevel: "info",
			},
			wantErr: errors.New("address is required"),
		},
		{
			name: "invalid mode",
			config: &Config{
				Addr:     ":8080",
				Mode:     "invalid",
				LogLevel: "info",
			},
			wantErr: errors.New("mode must be 'api' or 'worker'"),
		},
		{
			name: "invalid log level",
			config: &Config{
				Addr:     ":8080",
				Mode:     "api",
				LogLevel: "invalid",
			},
			wantErr: errors.New("invalid log level"),
		},
		{
			name: "worker mode valid",
			config: &Config{
				Addr:     ":8080",
				Mode:     "worker",
				LogLevel: "debug",
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
