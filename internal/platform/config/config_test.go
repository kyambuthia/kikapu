package config

import (
	"testing"
)

func TestValidateRateLimits(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid configuration",
			config: Config{
				RateLimitCheckout:     10,
				RateLimitAdmin:        120,
				RateLimitPublicRead:   300,
				RateLimitMaxBytes:     1048576,
			},
			wantErr: false,
		},
		{
			name: "invalid checkout limit",
			config: Config{
				RateLimitCheckout:     5,
				RateLimitAdmin:        120,
				RateLimitPublicRead:   300,
				RateLimitMaxBytes:     1048576,
			},
			wantErr: true,
		},
		{
			name: "invalid admin limit",
			config: Config{
				RateLimitCheckout:     10,
				RateLimitAdmin:        60,
				RateLimitPublicRead:   300,
				RateLimitMaxBytes:     1048576,
			},
			wantErr: true,
		},
		{
			name: "invalid public read limit",
			config: Config{
				RateLimitCheckout:     10,
				RateLimitAdmin:        120,
				RateLimitPublicRead:   150,
				RateLimitMaxBytes:     1048576,
			},
			wantErr: true,
		},
		{
			name: "max bytes too small",
			config: Config{
				RateLimitCheckout:     10,
				RateLimitAdmin:        120,
				RateLimitPublicRead:   300,
				RateLimitMaxBytes:     512,
			},
			wantErr: true,
		},
		{
			name: "max bytes at minimum",
			config: Config{
				RateLimitCheckout:     10,
				RateLimitAdmin:        120,
				RateLimitPublicRead:   300,
				RateLimitMaxBytes:     1024,
			},
			wantErr: false,
		},
		{
			name: "valid larger max bytes",
			config: Config{
				RateLimitCheckout:     10,
				RateLimitAdmin:        120,
				RateLimitPublicRead:   300,
				RateLimitMaxBytes:     5242880,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetEnvInt(t *testing.T) {
	t.Run("default value", func(t *testing.T) {
		value := getEnvInt("NONEXISTENT", 42)
		if value != 42 {
			t.Errorf("expected 42, got %d", value)
		}
	})
	t.Run("parse integer", func(t *testing.T) {
		value := getEnvInt("SOME_VAR", 0)
		if value != 42 {
			t.Errorf("expected 42, got %d", value)
		}
	})
	t.Run("invalid value returns default", func(t *testing.T) {
		value := getEnvInt("INVALID_VAR", 100)
		if value != 100 {
			t.Errorf("expected 100, got %d", value)
		}
	})
}

func TestGetEnvBool(t *testing.T) {
	t.Run("default value", func(t *testing.T) {
		value := getEnvBool("NONEXISTENT", false)
		if value != false {
			t.Errorf("expected false, got %v", value)
		}
	})
	t.Run("parse true", func(t *testing.T) {
		value := getEnvBool("SOME_VAR", false)
		if !value {
			t.Errorf("expected true, got %v", value)
		}
	})
	t.Run("parse false", func(t *testing.T) {
		value := getEnvBool("SOME_VAR", true)
		if value {
			t.Errorf("expected false, got %v", value)
		}
	})
}

func TestInSlice(t *testing.T) {
	tests := []struct {
		name   string
		slice  []int
		value  int
		result bool
	}{
		{"value in slice", []int{10, 20, 30}, 20, true},
		{"value not in slice", []int{10, 20, 30}, 25, false},
		{"empty slice", []int{}, 10, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := inSlice(tt.slice, tt.value)
			if result != tt.result {
				t.Errorf("inSlice() result = %v, want %v", result, tt.result)
			}
		})
	}
}

func TestToBool(t *testing.T) {
	tests := []struct {
		input  string
		result bool
	}{
		{"true", true},
		{"TRUE", true},
		{"1", true},
		{"false", false},
		{"FALSE", false},
		{"0", false},
		{"maybe", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := toBool(tt.input)
			if result != tt.result {
				t.Errorf("toBool(%v) = %v, want %v", tt.input, result, tt.result)
			}
		})
	}
}

func TestToLower(t *testing.T) {
	tests := []struct {
		input  string
		result string
	}{
		{"HELLO", "hello"},
		{"Hello", "hello"},
		{"hELLo", "hello"},
		{"123", "123"},
		{"", ""},
		{"UPPER", "upper"},
		{"MixedCASE", "mixedcase"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := toLower(tt.input)
			if result != tt.result {
				t.Errorf("toLower(%v) = %v, want %v", tt.input, result, tt.result)
			}
		})
	}
}
