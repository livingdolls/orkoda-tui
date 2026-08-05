package credentials

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.Set(ctx, "llm-provider:deepseek", "secret-value"); err != nil {
		t.Fatal(err)
	}
	value, err := store.Get(ctx, "llm-provider:deepseek")
	if err != nil {
		t.Fatal(err)
	}
	if value != "secret-value" {
		t.Fatalf("unexpected credential %q", value)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credential file permissions are %o", info.Mode().Perm())
	}
	if err := store.Delete(ctx, "llm-provider:deepseek"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "llm-provider:deepseek"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

type unavailableKeychainRunner struct{}

func (unavailableKeychainRunner) Run(context.Context, string, []string, string) (string, error) {
	return "", errors.New("desktop keyring session is unavailable")
}

func TestAutoStoreFallsBackWhenKeychainCommandCannotOperate(t *testing.T) {
	fallback, err := NewFileStore(filepath.Join(t.TempDir(), "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	store := &AutoStore{
		keychain: &Store{service: "orkoda", platform: "linux", runner: unavailableKeychainRunner{}},
		fallback: fallback,
	}
	ctx := context.Background()
	if err := store.Set(ctx, "llm-provider:openai", "provider-value"); err != nil {
		t.Fatal(err)
	}
	value, err := store.Get(ctx, "llm-provider:openai")
	if err != nil {
		t.Fatal(err)
	}
	if value != "provider-value" {
		t.Fatalf("unexpected fallback credential %q", value)
	}
	if err := store.Delete(ctx, "llm-provider:openai"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "llm-provider:openai"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected unavailable after deleting all stores, got %v", err)
	}
}
