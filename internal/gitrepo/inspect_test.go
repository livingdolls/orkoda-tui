package gitrepo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	responses map[string]string
	errors    map[string]error
}

func commandKey(arguments ...string) string {
	return strings.Join(arguments, "\x00")
}

func (f *fakeRunner) Run(_ context.Context, _ string, arguments ...string) (string, error) {
	key := commandKey(arguments...)
	if err := f.errors[key]; err != nil {
		return "", err
	}
	return f.responses[key], nil
}

func TestInspectReturnsRepositoryMetadata(t *testing.T) {
	root := t.TempDir()
	runner := &fakeRunner{responses: map[string]string{
		commandKey("rev-parse", "--show-toplevel"):                    root,
		commandKey("rev-parse", "HEAD"):                               "abc123",
		commandKey("branch", "--show-current"):                        "main",
		commandKey("status", "--porcelain", "--untracked-files=normal"): " M README.md",
		commandKey("remote", "get-url", "origin"):                     "git@github.com:livingdolls/example.git",
	}}

	snapshot, err := newInspector(runner).Inspect(context.Background(), root)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if snapshot.RootPath != filepath.Clean(root) {
		t.Fatalf("RootPath = %q, want %q", snapshot.RootPath, root)
	}
	if snapshot.CurrentBranch != "main" || snapshot.HeadSHA != "abc123" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.RemoteURL != "git@github.com:livingdolls/example.git" || !snapshot.Dirty {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestInspectAllowsRepositoryWithoutOrigin(t *testing.T) {
	root := t.TempDir()
	runner := &fakeRunner{
		responses: map[string]string{
			commandKey("rev-parse", "--show-toplevel"):                    root,
			commandKey("rev-parse", "HEAD"):                               "abc123",
			commandKey("branch", "--show-current"):                        "",
			commandKey("status", "--porcelain", "--untracked-files=normal"): "",
		},
		errors: map[string]error{
			commandKey("remote", "get-url", "origin"): errors.New("missing remote"),
		},
	}

	snapshot, err := newInspector(runner).Inspect(context.Background(), root)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if snapshot.RemoteURL != "" || snapshot.Dirty {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestInspectRejectsNonRepository(t *testing.T) {
	root := t.TempDir()
	runner := &fakeRunner{
		responses: map[string]string{},
		errors: map[string]error{
			commandKey("rev-parse", "--show-toplevel"): errors.New("not a repository"),
		},
	}

	if _, err := newInspector(runner).Inspect(context.Background(), root); err == nil {
		t.Fatal("Inspect() expected an error")
	}
}

func TestInspectRejectsFilePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := newInspector(&fakeRunner{}).Inspect(context.Background(), path); err == nil {
		t.Fatal("Inspect() expected an error")
	}
}
