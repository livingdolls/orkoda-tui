package execution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/livingdolls/orkoda-tui/internal/agentconfig"
)

func writablePolicy() agentconfig.ToolPolicy {
	return agentconfig.ToolPolicy{
		Role: agentconfig.RoleExecutor,
		AllowedTools: []string{
			agentconfig.ToolFileRead,
			agentconfig.ToolFileSearch,
			agentconfig.ToolFilePatch,
			agentconfig.ToolFileCreate,
			agentconfig.ToolFileDelete,
			agentconfig.ToolGitStatus,
			agentconfig.ToolGitDiff,
		},
		FilesystemAccess: agentconfig.FilesystemWorkspaceWrite,
		NetworkAccess:    agentconfig.NetworkDisabled,
		MaxFileBytes:     1024,
		MaxPatchBytes:    2048,
	}
}

func TestPathGuardRejectsEscapesAndGitInternals(t *testing.T) {
	root := t.TempDir()
	guard := PathGuard{}
	invalid := []string{"../secret", "/tmp/secret", ".git/config", "folder/../../secret"}
	for _, path := range invalid {
		t.Run(path, func(t *testing.T) {
			if _, err := guard.Resolve(root, path, true); !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("Resolve(%q) error = %v", path, err)
			}
		})
	}

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := guard.Resolve(root, "link/file.txt", true); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestToolsetCreatesPatchesReadsSearchesAndDeletes(t *testing.T) {
	root := t.TempDir()
	tools := Toolset{Root: root, Policy: writablePolicy()}
	if err := tools.Create("docs/example.txt", "alpha\nbeta\n"); err != nil {
		t.Fatal(err)
	}
	content, err := tools.Read("docs/example.txt")
	if err != nil || content != "alpha\nbeta\n" {
		t.Fatalf("Read() content = %q error = %v", content, err)
	}
	matches, err := tools.Search("beta", 10)
	if err != nil || len(matches) != 1 || matches[0] != "docs/example.txt:2" {
		t.Fatalf("Search() = %#v error = %v", matches, err)
	}
	if err := tools.Patch("docs/example.txt", "beta", "gamma"); err != nil {
		t.Fatal(err)
	}
	content, _ = tools.Read("docs/example.txt")
	if content != "alpha\ngamma\n" {
		t.Fatalf("patched content = %q", content)
	}
	if err := tools.Delete("docs/example.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "example.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted file error = %v", err)
	}
}

func TestToolsetEnforcesPolicyAndLimits(t *testing.T) {
	root := t.TempDir()
	policy := writablePolicy()
	policy.AllowedTools = []string{agentconfig.ToolFileRead}
	policy.MaxFileBytes = 4
	tools := Toolset{Root: root, Policy: policy}
	if err := tools.Create("blocked.txt", "x"); !errors.Is(err, ErrToolNotAllowed) {
		t.Fatalf("Create() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := tools.Read("large.txt"); !errors.Is(err, ErrSizeLimit) {
		t.Fatalf("Read large error = %v", err)
	}
}

func TestGitToolsUseWorkspaceRepository(t *testing.T) {
	root := t.TempDir()
	commands := [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	}
	tools := Toolset{Root: root, Policy: writablePolicy()}
	for _, command := range commands {
		if _, err := tools.git(context.Background(), command...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := tools.GitStatus(context.Background())
	if err != nil || status == "" {
		t.Fatalf("GitStatus() = %q error = %v", status, err)
	}
}
