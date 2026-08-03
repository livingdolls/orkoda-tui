package artifact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrInvalidKey = errors.New("invalid artifact key")

// LocalStore persists artifacts below one local root directory.
type LocalStore struct {
	root string
}

func NewLocalStore(root string) (*LocalStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("artifact root is required")
	}

	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact root: %w", err)
	}
	if info, err := os.Lstat(absoluteRoot); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("artifact root must be a regular directory")
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect artifact root: %w", err)
	}

	if err := os.MkdirAll(absoluteRoot, 0o750); err != nil {
		return nil, fmt.Errorf("create artifact root: %w", err)
	}
	if err := validateArtifactPath(absoluteRoot, absoluteRoot, false); err != nil {
		return nil, fmt.Errorf("validate artifact root: %w", err)
	}

	return &LocalStore{root: absoluteRoot}, nil
}

func (s *LocalStore) Save(ctx context.Context, key string, source io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	path, err := s.resolve(key)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}
	if err := validateArtifactPath(s.root, path, true); err != nil {
		return err
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".orkoda-artifact-*")
	if err != nil {
		return fmt.Errorf("create temporary artifact: %w", err)
	}

	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("set artifact permissions: %w", err)
	}

	if _, err := io.Copy(temporary, source); err != nil {
		temporary.Close()
		return fmt.Errorf("write artifact: %w", err)
	}

	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close artifact: %w", err)
	}
	if err := validateArtifactPath(s.root, path, true); err != nil {
		return err
	}

	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish artifact: %w", err)
	}

	return nil
}

func (s *LocalStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	path, err := s.resolve(key)
	if err != nil {
		return nil, err
	}

	file, err := openArtifactFile(path)
	if err != nil {
		return nil, fmt.Errorf("open artifact: %w", err)
	}

	return file, nil
}

func (s *LocalStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	path, err := s.resolve(key)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}

	return nil
}

func (s *LocalStore) resolve(key string) (string, error) {
	if strings.TrimSpace(key) == "" || filepath.IsAbs(key) {
		return "", ErrInvalidKey
	}

	cleaned := filepath.Clean(key)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", ErrInvalidKey
	}

	resolved := filepath.Join(s.root, cleaned)
	relative, err := filepath.Rel(s.root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrInvalidKey
	}
	if err := validateArtifactPath(s.root, resolved, true); err != nil {
		return "", err
	}

	return resolved, nil
}

func validateArtifactPath(root, target string, allowMissing bool) error {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	rootInfo, err := os.Lstat(root)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return ErrInvalidKey
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ErrInvalidKey
	}
	current := root
	if relative == "." {
		return nil
	}
	for index, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) && allowMissing {
			return nil
		}
		if statErr != nil {
			return fmt.Errorf("inspect artifact path: %w", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 || info.Mode()&(os.ModeDevice|os.ModeNamedPipe|os.ModeSocket) != 0 {
			return ErrInvalidKey
		}
		if index < len(strings.Split(relative, string(filepath.Separator)))-1 && !info.IsDir() {
			return ErrInvalidKey
		}
	}
	return nil
}
