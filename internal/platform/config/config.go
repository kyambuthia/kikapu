package config

import (
	"errors"
	"os"
)

type Config struct {
	Addr                string
	MetricsAddr         string
	Mode                string
	LogLevel            string
	PaymentProvider     string
	PaystackSecretKey   string
	PaystackBaseURL     string
	MpesaConsumerKey    string
	MpesaConsumerSecret string
	MpesaShortCode      string
	MpesaPasskey        string
	MpesaCallbackURL    string
	MpesaBaseURL        string
	MpesaAllowedCIDRs   string
}

func Load() (*Config, error) {
	cfg := &Config{
		Addr:                getEnv("KIKAPU_ADDR", ":8080"),
		MetricsAddr:         getEnv("KIKAPU_METRICS_ADDR", ":9090"),
		Mode:                getEnv("KIKAPU_MODE", "api"),
		LogLevel:            getEnv("KIKAPU_LOG_LEVEL", "info"),
		PaymentProvider:     getEnv("PAYMENT_PROVIDER", "fake"),
		PaystackSecretKey:   getEnv("PAYSTACK_SECRET_KEY", ""),
		PaystackBaseURL:     getEnv("PAYSTACK_BASE_URL", ""),
		MpesaConsumerKey:    getEnv("MPESA_CONSUMER_KEY", ""),
		MpesaConsumerSecret: getEnv("MPESA_CONSUMER_SECRET", ""),
		MpesaShortCode:      getEnv("MPESA_SHORT_CODE", ""),
		MpesaPasskey:        getEnv("MPESA_PASSKEY", ""),
		MpesaCallbackURL:    getEnv("MPESA_CALLBACK_URL", ""),
		MpesaBaseURL:        getEnv("MPESA_BASE_URL", ""),
		MpesaAllowedCIDRs:   getEnv("MPESA_ALLOWED_CIDRS", ""),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Addr == "" {
		return errors.New("address is required")
	}

	if c.Mode != "api" && c.Mode != "worker" {
		return errors.New("mode must be 'api' or 'worker'")
	}

	validLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLevels[c.LogLevel] {
		return errors.New("invalid log level")
	}

	if c.PaymentProvider != "fake" && c.PaymentProvider != "paystack" && c.PaymentProvider != "mpesa" {
		return errors.New("payment provider must be 'fake', 'paystack', or 'mpesa'")
	}

	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
