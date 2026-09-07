package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	defaultHTTPAddress     = ":8080"
	defaultShutdownTimeout = 5 * time.Second
)

type Config struct {
	HTTPAddress     string
	ShutdownTimeout time.Duration
}

func FromEnv() (Config, error) {
	config := Config{
		HTTPAddress:     envOrDefault("HTTP_ADDR", defaultHTTPAddress),
		ShutdownTimeout: defaultShutdownTimeout,
	}

	if rawTimeout := strings.TrimSpace(os.Getenv("SHUTDOWN_TIMEOUT")); rawTimeout != "" {
		timeout, err := time.ParseDuration(rawTimeout)
		if err != nil {
			return Config{}, fmt.Errorf("parse SHUTDOWN_TIMEOUT: %w", err)
		}
		if timeout <= 0 {
			return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT must be positive")
		}
		config.ShutdownTimeout = timeout
	}

	return config, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	return fallback
}
