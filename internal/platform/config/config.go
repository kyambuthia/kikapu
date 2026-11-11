package config

import (
	"errors"
	"os"
)

type Config struct {
	Addr                   string
	Mode                   string
	LogLevel               string
	PaymentProvider        string
	PaystackSecretKey      string
	PaystackBaseURL        string
	MpesaConsumerKey       string
	MpesaConsumerSecret    string
	MpesaShortCode         string
	MpesaPasskey           string
	MpesaCallbackURL       string
	MpesaBaseURL           string
	MpesaAllowedCIDRs      string
	RateLimitCheckout     int
	RateLimitAdmin        int
	RateLimitPublicRead   int
	RateLimitMaxBytes     int
	RateLimitTrustedProxies bool
}

func Load() (*Config, error) {
	cfg := &Config{
		Addr:                  getEnv("KIKAPU_ADDR", ":8080"),
		Mode:                  getEnv("KIKAPU_MODE", "api"),
		LogLevel:              getEnv("KIKAPU_LOG_LEVEL", "info"),
		PaymentProvider:       getEnv("PAYMENT_PROVIDER", "fake"),
		PaystackSecretKey:     getEnv("PAYSTACK_SECRET_KEY", ""),
		PaystackBaseURL:       getEnv("PAYSTACK_BASE_URL", ""),
		MpesaConsumerKey:      getEnv("MPESA_CONSUMER_KEY", ""),
		MpesaConsumerSecret:   getEnv("MPESA_CONSUMER_SECRET", ""),
		MpesaShortCode:        getEnv("MPESA_SHORT_CODE", ""),
		MpesaPasskey:          getEnv("MPESA_PASSKEY", ""),
		MpesaCallbackURL:      getEnv("MPESA_CALLBACK_URL", ""),
		MpesaBaseURL:          getEnv("MPESA_BASE_URL", ""),
		MpesaAllowedCIDRs:     getEnv("MPESA_ALLOWED_CIDRS", ""),
		RateLimitCheckout:     getEnvInt("KIKAPU_RATE_LIMIT_CHECKOUT", 10),
		RateLimitAdmin:        getEnvInt("KIKAPU_RATE_LIMIT_ADMIN", 120),
		RateLimitPublicRead:   getEnvInt("KIKAPU_RATE_LIMIT_PUBLIC_READ", 300),
		RateLimitMaxBytes:     getEnvInt("KIKAPU_RATE_LIMIT_MAX_BYTES", 1048576),
		RateLimitTrustedProxies: getEnvBool("KIKAPU_RATE_LIMIT_TRUSTED_PROXIES", false),
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
		return errors.New("mode must be \'api\' or \'worker\'")
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
		return errors.New("payment provider must be \'fake\', \'paystack\', or \'mpesa\'")
	}

	validLimits := []int{10, 120, 300}
	if !inSlice(validLimits, c.RateLimitCheckout) {
		return errors.New("rate_limit_checkout must be 10")
	}
	if !inSlice(validLimits, c.RateLimitAdmin) {
		return errors.New("rate_limit_admin must be 120")
	}
	if !inSlice(validLimits, c.RateLimitPublicRead) {
		return errors.New("rate_limit_public_read must be 300")
	}

	if c.RateLimitMaxBytes < 1024 {
		return errors.New("rate_limit_max_bytes must be at least 1024")
	}

	return nil
}

func inSlice(slice []int, value int) bool {
	for _, s := range slice {
		if s == value {
			return true
		}
	}
	return false
}

func getEnvInt(key string, defaultValue int) int {
	value := getEnv(key, "")
	if value == "" {
		return defaultValue
	}
	var result int
	_, err := literalInterpreter(value, &result)
	if err != nil {
		return defaultValue
	}
	return result
}

func getEnvBool(key string, defaultValue bool) bool {
	value := getEnv(key, "")
	if value == "" {
		return defaultValue
	}
	return toBool(value)
}

func literalInterpreter(input string, target interface{}) (bool, error) {
	switch v := target.(type) {
	case *int:
		*v = 0
		length := len(input)
		for i := 0; i < length; i++ {
			c := input[i]
			if c < '0' || c > '9' {
				return false, nil
			}
			*v = *v*10 + int(c-'0')
		}
		return length > 0, nil
	default:
		return false, nil
	}
}

func toBool(input string) bool {
	lower := toLower(input)
	return lower == "true" || lower == "1"
}

func toLower(input string) string {
	result := make([]byte, len(input))
	for i := 0; i < len(input); i++ {
		c := input[i]
		if c >= 'A' && c <= 'Z' {
			result[i] = c - 'A' + 'a'
		} else {
			result[i] = c
		}
	}
	return string(result)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
