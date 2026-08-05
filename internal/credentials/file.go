package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type CredentialStore interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string) error
	Delete(context.Context, string) error
}

type FileStore struct {
	path string
	mu   sync.Mutex
}

func NewFileStore(path string) (*FileStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("credential file path is required")
	}
	return &FileStore{path: filepath.Clean(path)}, nil
}

func (s *FileStore) Get(_ context.Context, account string) (string, error) {
	if s == nil {
		return "", ErrUnavailable
	}
	account = strings.TrimSpace(account)
	if account == "" {
		return "", fmt.Errorf("credential account is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.read()
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(values[account])
	if value == "" {
		return "", ErrNotFound
	}
	return value, nil
}

func (s *FileStore) Set(_ context.Context, account, value string) error {
	if s == nil {
		return ErrUnavailable
	}
	account = strings.TrimSpace(account)
	value = strings.TrimSpace(value)
	if account == "" || value == "" || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("credential account and a single-line value are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.read()
	if err != nil {
		return err
	}
	values[account] = value
	return s.write(values)
}

func (s *FileStore) Delete(_ context.Context, account string) error {
	if s == nil {
		return ErrUnavailable
	}
	account = strings.TrimSpace(account)
	if account == "" {
		return fmt.Errorf("credential account is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.read()
	if err != nil {
		return err
	}
	delete(values, account)
	return s.write(values)
}

func (s *FileStore) read() (map[string]string, error) {
	content, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read protected credential file: %w", err)
	}
	values := map[string]string{}
	if len(content) > 0 {
		if err := json.Unmarshal(content, &values); err != nil {
			return nil, fmt.Errorf("decode protected credential file: %w", err)
		}
	}
	return values, nil
}

func (s *FileStore) write(values map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create credential directory: %w", err)
	}
	content, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("encode protected credential file: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), ".credentials-*")
	if err != nil {
		return fmt.Errorf("create temporary credential file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("protect temporary credential file: %w", err)
	}
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return fmt.Errorf("write temporary credential file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync temporary credential file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary credential file: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("replace protected credential file: %w", err)
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		return fmt.Errorf("protect credential file: %w", err)
	}
	return nil
}

type AutoStore struct {
	keychain *Store
	fallback *FileStore
}

func NewAutoStore(service, fallbackPath string) (*AutoStore, error) {
	keychain, err := NewOSStore(service)
	if err != nil {
		return nil, err
	}
	fallback, err := NewFileStore(fallbackPath)
	if err != nil {
		return nil, err
	}
	return &AutoStore{keychain: keychain, fallback: fallback}, nil
}

func (s *AutoStore) Get(ctx context.Context, account string) (string, error) {
	if s == nil || s.keychain == nil || s.fallback == nil {
		return "", ErrUnavailable
	}
	value, keychainErr := s.keychain.Get(ctx, account)
	if keychainErr == nil {
		return value, nil
	}
	value, fallbackErr := s.fallback.Get(ctx, account)
	if fallbackErr == nil {
		return value, nil
	}
	if !errors.Is(fallbackErr, ErrNotFound) {
		return "", fallbackErr
	}
	if errors.Is(keychainErr, ErrNotFound) {
		return "", ErrNotFound
	}
	return "", ErrUnavailable
}

func (s *AutoStore) Set(ctx context.Context, account, value string) error {
	if s == nil || s.keychain == nil || s.fallback == nil {
		return ErrUnavailable
	}
	if err := s.keychain.Set(ctx, account, value); err == nil {
		_ = s.fallback.Delete(ctx, account)
		return nil
	}
	// A desktop keychain command may exist but still be unusable because the
	// session has no DBus service, the keyring is locked, or the user declined
	// access. Preserve usability by falling back to the owner-only local store.
	return s.fallback.Set(ctx, account, value)
}

func (s *AutoStore) Delete(ctx context.Context, account string) error {
	if s == nil || s.keychain == nil || s.fallback == nil {
		return ErrUnavailable
	}
	_ = s.keychain.Delete(ctx, account)
	return s.fallback.Delete(ctx, account)
}

var _ CredentialStore = (*Store)(nil)
var _ CredentialStore = (*FileStore)(nil)
var _ CredentialStore = (*AutoStore)(nil)
