package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeManagerPreparesDetachedWorktreeIdempotently(t *testing.T) {
	ctx := context.Background()
	source, head := createGitRepository(t)
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	manager := NewWorktreeManager()

	first, err := manager.Prepare(ctx, source, workspacePath, head)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if first.HeadSHA != head || first.Dirty {
		t.Fatalf("first snapshot = %#v", first)
	}
	branch := runGit(t, workspacePath, "branch", "--show-current")
	if branch != "" {
		t.Fatalf("workspace branch = %q, want detached HEAD", branch)
	}

	second, err := manager.Prepare(ctx, source, workspacePath, head)
	if err != nil {
		t.Fatalf("idempotent Prepare() error = %v", err)
	}
	if second.Path != first.Path || second.HeadSHA != first.HeadSHA {
		t.Fatalf("second snapshot = %#v, first = %#v", second, first)
	}

	if err := manager.Remove(ctx, source, workspacePath); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Stat(workspacePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace path still exists: %v", err)
	}
}

func TestWorktreeManagerRejectsDirtySource(t *testing.T) {
	source, head := createGitRepository(t)
	if err := os.WriteFile(filepath.Join(source, "dirty.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewWorktreeManager().Prepare(
		context.Background(), source, filepath.Join(t.TempDir(), "workspace"), head,
	)
	if !errors.Is(err, ErrSourceDirty) {
		t.Fatalf("Prepare() error = %v", err)
	}
}

func TestWorktreeManagerRejectsNestedWorkspacePath(t *testing.T) {
	source, head := createGitRepository(t)
	_, err := NewWorktreeManager().Prepare(
		context.Background(), source, filepath.Join(source, ".orkoda", "workspace"), head,
	)
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("Prepare() error = %v", err)
	}
}

func TestWorktreeManagerRejectsMismatchedExistingWorkspace(t *testing.T) {
	ctx := context.Background()
	source, firstHead := createGitRepository(t)
	if err := os.WriteFile(filepath.Join(source, "second.txt"), []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "add", "second.txt")
	runGit(t, source, "commit", "-m", "second")
	secondHead := runGit(t, source, "rev-parse", "HEAD")
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	manager := NewWorktreeManager()
	if _, err := manager.Prepare(ctx, source, workspacePath, firstHead); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Inspect(ctx, workspacePath, secondHead); !errors.Is(err, ErrWorkspaceMismatch) {
		t.Fatalf("Inspect() error = %v", err)
	}
}

func createGitRepository(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.name", "Orkoda Test")
	runGit(t, root, "config", "user.email", "orkoda@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "initial")
	return root, runGit(t, root, "rev-parse", "HEAD")
}

func runGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	commandArguments := append([]string{"-C", directory}, arguments...)
	command := exec.Command("git", commandArguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(arguments, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
