package execution

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectContextFilesSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.go"), []byte("package secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.go"), filepath.Join(root, "linked.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "safe.go"), []byte("package safe\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := collectContextFiles(root, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "safe.go" {
		t.Fatalf("files = %v", files)
	}
}
