package config

import "testing"

func TestLoadUsesDefaults(t *testing.T) {
	t.Setenv("ORKODA_ENV", "")
	t.Setenv("ORKODA_LOG_LEVEL", "")
	t.Setenv("ORKODA_API_HOST", "")
	t.Setenv("ORKODA_API_PORT", "")
	t.Setenv("ORKODA_SHUTDOWN_TIMEOUT", "")
	t.Setenv("ORKODA_DATA_DIR", "")
	t.Setenv("ORKODA_ARTIFACT_DIR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.APIAddress() != "127.0.0.1:8181" {
		t.Fatalf("APIAddress() = %q", cfg.APIAddress())
	}

	if cfg.DataDir != ".orkoda" {
		t.Fatalf("DataDir = %q", cfg.DataDir)
	}

	if cfg.ArtifactDir != ".orkoda/artifacts" {
		t.Fatalf("ArtifactDir = %q", cfg.ArtifactDir)
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	t.Setenv("ORKODA_API_PORT", "70000")

	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an error")
	}
}
