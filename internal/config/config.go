package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPAddress     = ":8080"
	defaultShutdownTimeout = 5 * time.Second
)

type Config struct {
	HTTPAddress                   string
	ShutdownTimeout               time.Duration
	DatabaseURL                   string
	SupplierURL                   string
	SupplierBURL                  string
	SupplierAfterIssueDelay       time.Duration
	SupplierForceFinalUnavailable bool
}

func FromEnv() (Config, error) {
	config := Config{
		HTTPAddress:     envOrDefault("HTTP_ADDR", defaultHTTPAddress),
		ShutdownTimeout: defaultShutdownTimeout,
		DatabaseURL:     strings.TrimSpace(os.Getenv("DATABASE_URL")),
		SupplierURL:     envOrDefault("SUPPLIER_URL", "http://127.0.0.1:8081"),
		SupplierBURL:    envOrDefault("SUPPLIER_B_URL", "http://127.0.0.1:8082"),
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
	if rawDelay := strings.TrimSpace(os.Getenv("SUPPLIER_AFTER_ISSUE_DELAY")); rawDelay != "" {
		delay, err := time.ParseDuration(rawDelay)
		if err != nil {
			return Config{}, fmt.Errorf("parse SUPPLIER_AFTER_ISSUE_DELAY: %w", err)
		}
		if delay < 0 {
			return Config{}, fmt.Errorf("SUPPLIER_AFTER_ISSUE_DELAY must not be negative")
		}
		config.SupplierAfterIssueDelay = delay
	}
	if rawUnavailable := strings.TrimSpace(os.Getenv("SUPPLIER_FORCE_FINAL_UNAVAILABLE")); rawUnavailable != "" {
		forceUnavailable, err := strconv.ParseBool(rawUnavailable)
		if err != nil {
			return Config{}, fmt.Errorf("parse SUPPLIER_FORCE_FINAL_UNAVAILABLE: %w", err)
		}
		config.SupplierForceFinalUnavailable = forceUnavailable
	}

	return config, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	return fallback
}
