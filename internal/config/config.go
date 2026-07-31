package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultAPIHost         = "127.0.0.1"
	defaultAPIPort         = 8181
	defaultEnvironment     = "development"
	defaultLogLevel        = "info"
	defaultShutdownTimeout = 10 * time.Second
)

// Config contains process-level settings shared by the local daemon and workers.
type Config struct {
	Environment     string
	LogLevel        string
	APIHost         string
	APIPort         int
	ShutdownTimeout time.Duration
	DatabaseURL     string
	RedisURL        string
	RabbitMQURL     string
}

// Load reads environment variables and applies safe local-development defaults.
func Load() (Config, error) {
	port, err := intFromEnv("ORKODA_API_PORT", defaultAPIPort)
	if err != nil {
		return Config{}, err
	}

	shutdownTimeout, err := durationFromEnv("ORKODA_SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Environment:     stringFromEnv("ORKODA_ENV", defaultEnvironment),
		LogLevel:        stringFromEnv("ORKODA_LOG_LEVEL", defaultLogLevel),
		APIHost:         stringFromEnv("ORKODA_API_HOST", defaultAPIHost),
		APIPort:         port,
		ShutdownTimeout: shutdownTimeout,
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		RedisURL:        os.Getenv("REDIS_URL"),
		RabbitMQURL:     os.Getenv("RABBITMQ_URL"),
	}, nil
}

func (c Config) APIAddress() string {
	return fmt.Sprintf("%s:%d", c.APIHost, c.APIPort)
}

func stringFromEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func intFromEnv(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}

	if parsed < 1 || parsed > 65535 {
		return 0, fmt.Errorf("%s must be between 1 and 65535", key)
	}

	return parsed, nil
}

func durationFromEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}

	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", key)
	}

	return parsed, nil
}
