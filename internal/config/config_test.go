package config

import "testing"

func TestLoadUsesDefaults(t *testing.T) {
	t.Setenv("ORKODA_ENV", "")
	t.Setenv("ORKODA_LOG_LEVEL", "")
	t.Setenv("ORKODA_API_HOST", "")
	t.Setenv("ORKODA_API_PORT", "")
	t.Setenv("ORKODA_SHUTDOWN_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.APIAddress() != "127.0.0.1:8181" {
		t.Fatalf("APIAddress() = %q", cfg.APIAddress())
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	t.Setenv("ORKODA_API_PORT", "70000")

	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an error")
	}
}
