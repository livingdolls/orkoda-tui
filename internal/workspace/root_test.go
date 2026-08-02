package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareRootCreatesAndCanonicalizesDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "managed", "workspaces")
	resolved, err := PrepareRoot(root)
	if err != nil {
		t.Fatalf("PrepareRoot() error = %v", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		t.Fatalf("workspace root stat = %v, info = %#v", err, info)
	}
	if !filepath.IsAbs(resolved) {
		t.Fatalf("resolved root = %q, want absolute", resolved)
	}
}

func TestPrepareRootRejectsSymlink(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "workspaces")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink is unavailable: %v", err)
	}
	if _, err := PrepareRoot(link); err == nil {
		t.Fatal("PrepareRoot() expected a symlink error")
	}
}

func TestPrepareRootRejectsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspaces")
	if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareRoot(path); err == nil {
		t.Fatal("PrepareRoot() expected a file error")
	}
}
