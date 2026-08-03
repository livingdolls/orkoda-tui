package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUsesDefaults(t *testing.T) {
	clearConfigEnvironment(t)

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
	if cfg.DatabasePath != ".orkoda/orkoda.db" {
		t.Fatalf("DatabasePath = %q", cfg.DatabasePath)
	}
	if cfg.ArtifactDir != ".orkoda/artifacts" {
		t.Fatalf("ArtifactDir = %q", cfg.ArtifactDir)
	}
	if cfg.WorkspaceDir != ".orkoda/workspaces" || cfg.WorkspaceLeaseTTL.String() != "5m0s" {
		t.Fatalf("unexpected workspace config: dir=%q lease=%s", cfg.WorkspaceDir, cfg.WorkspaceLeaseTTL)
	}
	if cfg.LLM.Provider != "local-fake" || cfg.LLM.Timeout.String() != "1m0s" {
		t.Fatalf("unexpected default LLM config %#v", cfg.LLM)
	}
}

func TestLoadReadsOpenAICompatibleConfig(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("ORKODA_LLM_PROVIDER", "OpenRouter")
	t.Setenv("ORKODA_LLM_BASE_URL", "https://example.com/v1")
	t.Setenv("ORKODA_LLM_API_KEY", "secret")
	t.Setenv("ORKODA_LLM_MODEL", "example/model")
	t.Setenv("ORKODA_LLM_JSON_MODE", "json_object")
	t.Setenv("ORKODA_LLM_TIMEOUT", "45s")
	t.Setenv("ORKODA_LLM_HEADERS_JSON", `{"HTTP-Referer":"https://orkoda.local","X-Title":"Orkoda"}`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Provider != "openrouter" || cfg.LLM.Model != "example/model" || cfg.LLM.Timeout.String() != "45s" {
		t.Fatalf("unexpected LLM config %#v", cfg.LLM)
	}
	if cfg.LLM.Headers["X-Title"] != "Orkoda" {
		t.Fatalf("unexpected headers %#v", cfg.LLM.Headers)
	}
}

func TestLoadReadsWorkspaceConfig(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("ORKODA_WORKSPACE_DIR", "/tmp/orkoda-workspaces")
	t.Setenv("ORKODA_WORKSPACE_LEASE_TTL", "90s")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WorkspaceDir != "/tmp/orkoda-workspaces" || cfg.WorkspaceLeaseTTL.String() != "1m30s" {
		t.Fatalf("unexpected workspace config: dir=%q lease=%s", cfg.WorkspaceDir, cfg.WorkspaceLeaseTTL)
	}
}

func TestLoadDerivesRuntimePathsFromCustomDataDir(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("ORKODA_DATA_DIR", "/tmp/orkoda-custom")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabasePath != "/tmp/orkoda-custom/orkoda.db" ||
		cfg.ArtifactDir != "/tmp/orkoda-custom/artifacts" ||
		cfg.WorkspaceDir != "/tmp/orkoda-custom/workspaces" ||
		cfg.APITokenFile != "/tmp/orkoda-custom/api.token" {
		t.Fatalf("derived runtime paths = %#v", cfg)
	}
}

func TestLoadRejectsIncompleteLLMConfig(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("ORKODA_LLM_PROVIDER", "openrouter")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an incomplete LLM config error")
	}

	clearConfigEnvironment(t)
	t.Setenv("ORKODA_LLM_HEADERS_JSON", `{invalid`)
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an invalid header JSON error")
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("ORKODA_API_PORT", "70000")

	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an error")
	}
}

func TestEnsureAPITokenCreatesPrivateStableToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api.token")
	first, err := EnsureAPIToken(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 {
		t.Fatalf("token length = %d", len(first))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token permissions = %o", info.Mode().Perm())
	}
	second, err := EnsureAPIToken(path, "")
	if err != nil || second != first {
		t.Fatalf("stable token = %q error=%v", second, err)
	}
}

func TestLoadRejectsInvalidWorkspaceLeaseTTL(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("ORKODA_WORKSPACE_LEASE_TTL", "0s")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected a workspace lease TTL error")
	}
}

func clearConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ORKODA_ENV",
		"ORKODA_LOG_LEVEL",
		"ORKODA_API_HOST",
		"ORKODA_API_PORT",
		"ORKODA_SHUTDOWN_TIMEOUT",
		"ORKODA_DATA_DIR",
		"ORKODA_DATABASE_PATH",
		"ORKODA_ARTIFACT_DIR",
		"ORKODA_WORKSPACE_DIR",
		"ORKODA_WORKSPACE_LEASE_TTL",
		"ORKODA_API_TOKEN",
		"ORKODA_API_TOKEN_FILE",
		"ORKODA_SANDBOX_MODE",
		"ORKODA_ALLOW_UNSANDBOXED_CHECKS",
		"ORKODA_LLM_PROVIDER",
		"ORKODA_LLM_BASE_URL",
		"ORKODA_LLM_API_KEY",
		"ORKODA_LLM_MODEL",
		"ORKODA_LLM_JSON_MODE",
		"ORKODA_LLM_TIMEOUT",
		"ORKODA_LLM_HEADERS_JSON",
	} {
		t.Setenv(key, "")
	}
}
