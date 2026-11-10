package config

import (
	"errors"
	"os"
	"strconv"
)

type Config struct {
	Addr     string
	Mode     string
	LogLevel string
}

func Load() (*Config, error) {
	cfg := &Config{
		Addr:     getEnv("KIKAPU_ADDR", ":8080"),
		Mode:     getEnv("KIKAPU_MODE", "api"),
		LogLevel: getEnv("KIKAPU_LOG_LEVEL", "info"),
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

	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}
