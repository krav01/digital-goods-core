package config

import (
	"testing"
	"time"
)

func TestFromEnv(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("SHUTDOWN_TIMEOUT", "12s")
	t.Setenv("SUPPLIER_AFTER_ISSUE_DELAY", "3s")
	t.Setenv("SUPPLIER_FINAL_UNAVAILABLE_PERCENT", "25")

	got, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}

	if got.HTTPAddress != ":9090" {
		t.Errorf("HTTPAddress = %q, want %q", got.HTTPAddress, ":9090")
	}
	if got.ShutdownTimeout != 12*time.Second {
		t.Errorf("ShutdownTimeout = %s, want %s", got.ShutdownTimeout, 12*time.Second)
	}
	if got.SupplierAfterIssueDelay != 3*time.Second {
		t.Errorf("SupplierAfterIssueDelay = %s, want %s", got.SupplierAfterIssueDelay, 3*time.Second)
	}
	if got.SupplierFinalUnavailableRate != 25 {
		t.Errorf("SupplierFinalUnavailableRate = %d, want 25", got.SupplierFinalUnavailableRate)
	}
}

func TestFromEnvInvalidSupplierAfterIssueDelay(t *testing.T) {
	t.Setenv("SUPPLIER_AFTER_ISSUE_DELAY", "-1s")

	if _, err := FromEnv(); err == nil {
		t.Fatal("FromEnv() error = nil, want error")
	}
}

func TestFromEnvInvalidShutdownTimeout(t *testing.T) {
	t.Setenv("SHUTDOWN_TIMEOUT", "0s")

	if _, err := FromEnv(); err == nil {
		t.Fatal("FromEnv() error = nil, want error")
	}
}

func TestFromEnvInvalidSupplierFinalUnavailablePercent(t *testing.T) {
	for _, value := range []string{"invalid", "-1", "101"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("SUPPLIER_FINAL_UNAVAILABLE_PERCENT", value)

			if _, err := FromEnv(); err == nil {
				t.Fatal("FromEnv() error = nil, want error")
			}
		})
	}
}
