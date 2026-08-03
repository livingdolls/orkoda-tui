package artifact

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStoreLifecycle(t *testing.T) {
	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalStore() error = %v", err)
	}

	ctx := context.Background()
	const key = "executions/job-1/result.patch"
	const content = "diff --git a/file.go b/file.go"

	if err := store.Save(ctx, key, strings.NewReader(content)); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	reader, err := store.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	stored, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if string(stored) != content {
		t.Fatalf("stored content = %q", stored)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if _, err := store.Open(ctx, key); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open() after Delete() error = %v", err)
	}
}

func TestLocalStoreRejectsTraversal(t *testing.T) {
	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalStore() error = %v", err)
	}

	for _, key := range []string{"", "../secret", "nested/../../secret"} {
		err := store.Save(context.Background(), key, strings.NewReader("secret"))
		if !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("Save(%q) error = %v", key, err)
		}
	}
}

func TestLocalStoreRejectsSymlinkedKeyPath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	store, err := NewLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "escape/secret.txt", strings.NewReader("secret")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Save() through symlink error = %v", err)
	}
}
