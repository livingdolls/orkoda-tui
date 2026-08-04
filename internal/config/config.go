package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAPIHost           = "127.0.0.1"
	defaultAPIPort           = 8181
	defaultEnvironment       = "development"
	defaultLogLevel          = "info"
	defaultShutdownTimeout   = 10 * time.Second
	defaultDataDir           = ".orkoda"
	defaultDatabasePath      = ".orkoda/orkoda.db"
	defaultArtifactDir       = ".orkoda/artifacts"
	defaultWorkspaceDir      = ".orkoda/workspaces"
	defaultWorkspaceLeaseTTL = 5 * time.Minute
	defaultSandboxMode       = "docker"
	defaultSandboxImage      = "orkoda-sandbox:local"
)

// Config contains process-level settings for the local daemon.
type Config struct {
	Environment       string
	LogLevel          string
	APIHost           string
	APIPort           int
	ShutdownTimeout   time.Duration
	DataDir           string
	DatabasePath      string
	ArtifactDir       string
	WorkspaceDir      string
	APISocket         string
	WorkspaceLeaseTTL time.Duration
	APIToken          string
	APITokenFile      string
	SandboxMode       string
	SandboxImage      string
	AllowUnsandboxed  bool
	LLM               LLMConfig
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
	workspaceLeaseTTL, err := durationFromEnv("ORKODA_WORKSPACE_LEASE_TTL", defaultWorkspaceLeaseTTL)
	if err != nil {
		return Config{}, err
	}
	llmConfig, err := loadLLMConfig()
	if err != nil {
		return Config{}, err
	}

	allowUnsandboxed, err := boolFromEnv("ORKODA_ALLOW_UNSANDBOXED_CHECKS", false)
	if err != nil {
		return Config{}, err
	}
	dataDir := stringFromEnv("ORKODA_DATA_DIR", defaultDataDir)
	apiTokenFile := stringFromEnv("ORKODA_API_TOKEN_FILE", filepath.Join(dataDir, "api.token"))
	sandboxMode := strings.ToLower(strings.TrimSpace(stringFromEnv("ORKODA_SANDBOX_MODE", defaultSandboxMode)))
	if sandboxMode != "docker" && sandboxMode != "host" {
		return Config{}, fmt.Errorf("ORKODA_SANDBOX_MODE must be docker or host")
	}
	if sandboxMode == "host" && !allowUnsandboxed {
		return Config{}, fmt.Errorf("host check execution requires ORKODA_ALLOW_UNSANDBOXED_CHECKS=true")
	}
	databasePath := stringFromEnv("ORKODA_DATABASE_PATH", filepath.Join(dataDir, filepath.Base(defaultDatabasePath)))
	artifactDir := stringFromEnv("ORKODA_ARTIFACT_DIR", filepath.Join(dataDir, filepath.Base(defaultArtifactDir)))
	workspaceDir := stringFromEnv("ORKODA_WORKSPACE_DIR", filepath.Join(dataDir, filepath.Base(defaultWorkspaceDir)))
	apiSocket := stringFromEnv("ORKODA_API_SOCKET", filepath.Join(dataDir, "orkoda.sock"))
	return Config{
		Environment:       stringFromEnv("ORKODA_ENV", defaultEnvironment),
		LogLevel:          stringFromEnv("ORKODA_LOG_LEVEL", defaultLogLevel),
		APIHost:           stringFromEnv("ORKODA_API_HOST", defaultAPIHost),
		APIPort:           port,
		ShutdownTimeout:   shutdownTimeout,
		DataDir:           dataDir,
		DatabasePath:      databasePath,
		ArtifactDir:       artifactDir,
		WorkspaceDir:      workspaceDir,
		APISocket:         apiSocket,
		WorkspaceLeaseTTL: workspaceLeaseTTL,
		APIToken:          strings.TrimSpace(os.Getenv("ORKODA_API_TOKEN")),
		APITokenFile:      apiTokenFile,
		SandboxMode:       sandboxMode,
		SandboxImage:      stringFromEnv("ORKODA_SANDBOX_IMAGE", defaultSandboxImage),
		AllowUnsandboxed:  allowUnsandboxed,
		LLM:               llmConfig,
	}, nil
}

// EnsureAPIToken returns the configured token or creates a mode-0600 local
// token file for the daemon/TUI pair. The token is never logged.
func EnsureAPIToken(path, configured string) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	configured = strings.TrimSpace(configured)
	if configured != "" {
		if len(configured) < 32 || strings.ContainsAny(configured, "\r\n \t") {
			return "", fmt.Errorf("ORKODA_API_TOKEN must contain at least 32 non-whitespace characters")
		}
		if err := writeTokenFile(path, configured); err != nil {
			return "", err
		}
		return configured, nil
	}
	if path == "" || path == "." {
		return "", fmt.Errorf("API token file path is required")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("API token file must be a regular non-symlink file")
		}
		payload, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", fmt.Errorf("read API token file: %w", readErr)
		}
		token := strings.TrimSpace(string(payload))
		if len(token) < 32 || strings.ContainsAny(token, "\r\n \t") {
			return "", fmt.Errorf("API token file contains an invalid token")
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return "", fmt.Errorf("restrict API token file permissions: %w", err)
		}
		return token, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect API token file: %w", err)
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate API token: %w", err)
	}
	token := hex.EncodeToString(raw[:])
	if err := writeTokenFile(path, token); err != nil {
		return "", err
	}
	return token, nil
}

func writeTokenFile(path, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create API token directory: %w", err)
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write API token through symlink")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".orkoda-api-token-*")
	if err != nil {
		return fmt.Errorf("create temporary API token file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.WriteString(token + "\n"); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish API token file: %w", err)
	}
	return nil
}

func (c Config) APIAddress() string {
	return fmt.Sprintf("%s:%d", c.APIHost, c.APIPort)
}

func (c Config) APISocketPath() string {
	return filepath.Clean(strings.TrimSpace(c.APISocket))
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

func boolFromEnv(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}
