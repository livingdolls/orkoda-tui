package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrSourceDirty       = errors.New("source repository has uncommitted changes")
	ErrBaseCommitMissing = errors.New("base commit is unavailable")
	ErrUnsafePath        = errors.New("workspace path overlaps the source repository")
	ErrWorkspaceMismatch = errors.New("existing workspace does not match the pinned base commit")
)

type WorktreeSnapshot struct {
	Path    string
	HeadSHA string
	Dirty   bool
}

type gitRunner interface {
	Run(context.Context, string, ...string) (string, error)
}

type commandRunner struct{}

func (commandRunner) Run(ctx context.Context, directory string, arguments ...string) (string, error) {
	commandArguments := append([]string{"-C", directory}, arguments...)
	command := exec.CommandContext(ctx, "git", commandArguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return "", fmt.Errorf("git %s: %s", strings.Join(arguments, " "), message)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(arguments, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}

type WorktreeManager struct {
	runner gitRunner
}

func NewWorktreeManager() *WorktreeManager {
	return &WorktreeManager{runner: commandRunner{}}
}

func newWorktreeManager(runner gitRunner) *WorktreeManager {
	return &WorktreeManager{runner: runner}
}

func (m *WorktreeManager) Prepare(
	ctx context.Context,
	sourcePath string,
	workspacePath string,
	baseCommitSHA string,
) (WorktreeSnapshot, error) {
	sourceRoot, err := resolveDirectory(sourcePath)
	if err != nil {
		return WorktreeSnapshot{}, fmt.Errorf("resolve source repository: %w", err)
	}
	workspaceAbsolute, err := filepath.Abs(strings.TrimSpace(workspacePath))
	if err != nil {
		return WorktreeSnapshot{}, fmt.Errorf("resolve workspace path: %w", err)
	}
	workspaceAbsolute = filepath.Clean(workspaceAbsolute)
	if pathsOverlap(sourceRoot, workspaceAbsolute) {
		return WorktreeSnapshot{}, ErrUnsafePath
	}

	canonicalBase, err := m.runner.Run(ctx, sourceRoot, "rev-parse", "--verify", baseCommitSHA+"^{commit}")
	if err != nil {
		return WorktreeSnapshot{}, fmt.Errorf("%w: %v", ErrBaseCommitMissing, err)
	}
	status, err := m.runner.Run(ctx, sourceRoot, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return WorktreeSnapshot{}, fmt.Errorf("inspect source repository status: %w", err)
	}
	if status != "" {
		return WorktreeSnapshot{}, ErrSourceDirty
	}

	if _, err := os.Lstat(workspaceAbsolute); err == nil {
		return m.Inspect(ctx, workspaceAbsolute, canonicalBase)
	} else if !errors.Is(err, os.ErrNotExist) {
		return WorktreeSnapshot{}, fmt.Errorf("inspect workspace path: %w", err)
	}

	parent := filepath.Dir(workspaceAbsolute)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return WorktreeSnapshot{}, fmt.Errorf("create workspace parent: %w", err)
	}
	if _, err := m.runner.Run(ctx, sourceRoot, "worktree", "prune", "--expire", "now"); err != nil {
		return WorktreeSnapshot{}, fmt.Errorf("prune stale Git worktrees: %w", err)
	}
	if _, err := m.runner.Run(ctx, sourceRoot, "worktree", "add", "--detach", workspaceAbsolute, canonicalBase); err != nil {
		return WorktreeSnapshot{}, fmt.Errorf("create detached Git worktree: %w", err)
	}

	snapshot, err := m.Inspect(ctx, workspaceAbsolute, canonicalBase)
	if err == nil {
		return snapshot, nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	cleanupErr := m.Remove(cleanupCtx, sourceRoot, workspaceAbsolute)
	if cleanupErr != nil {
		return WorktreeSnapshot{}, fmt.Errorf("verify new worktree: %v; cleanup: %w", err, cleanupErr)
	}
	return WorktreeSnapshot{}, err
}

func (m *WorktreeManager) Inspect(
	ctx context.Context,
	workspacePath string,
	baseCommitSHA string,
) (WorktreeSnapshot, error) {
	workspaceRoot, err := resolveDirectory(workspacePath)
	if err != nil {
		return WorktreeSnapshot{}, fmt.Errorf("resolve existing workspace: %w", err)
	}
	root, err := m.runner.Run(ctx, workspaceRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return WorktreeSnapshot{}, fmt.Errorf("workspace is not a Git worktree: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return WorktreeSnapshot{}, fmt.Errorf("resolve worktree root: %w", err)
	}
	if filepath.Clean(resolvedRoot) != workspaceRoot {
		return WorktreeSnapshot{}, ErrWorkspaceMismatch
	}
	head, err := m.runner.Run(ctx, workspaceRoot, "rev-parse", "HEAD")
	if err != nil {
		return WorktreeSnapshot{}, fmt.Errorf("read workspace HEAD: %w", err)
	}
	expected, err := m.runner.Run(ctx, workspaceRoot, "rev-parse", "--verify", baseCommitSHA+"^{commit}")
	if err != nil {
		return WorktreeSnapshot{}, fmt.Errorf("resolve expected workspace commit: %w", err)
	}
	if head != expected {
		return WorktreeSnapshot{}, fmt.Errorf("%w: HEAD %s, expected %s", ErrWorkspaceMismatch, head, expected)
	}
	status, err := m.runner.Run(ctx, workspaceRoot, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return WorktreeSnapshot{}, fmt.Errorf("inspect workspace status: %w", err)
	}
	return WorktreeSnapshot{Path: workspaceRoot, HeadSHA: head, Dirty: status != ""}, nil
}

func (m *WorktreeManager) Remove(ctx context.Context, sourcePath, workspacePath string) error {
	sourceRoot, err := resolveDirectory(sourcePath)
	if err != nil {
		return fmt.Errorf("resolve source repository: %w", err)
	}
	workspaceAbsolute, err := filepath.Abs(workspacePath)
	if err != nil {
		return fmt.Errorf("resolve workspace path: %w", err)
	}
	if pathsOverlap(sourceRoot, workspaceAbsolute) {
		return ErrUnsafePath
	}
	if _, err := m.runner.Run(ctx, sourceRoot, "worktree", "remove", "--force", filepath.Clean(workspaceAbsolute)); err != nil {
		if removeErr := os.RemoveAll(workspaceAbsolute); removeErr != nil {
			return fmt.Errorf("remove Git worktree: %v; remove directory: %w", err, removeErr)
		}
	}
	if _, err := m.runner.Run(ctx, sourceRoot, "worktree", "prune", "--expire", "now"); err != nil {
		return fmt.Errorf("prune removed Git worktree: %w", err)
	}
	return nil
}

func resolveDirectory(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("symlink paths are not allowed")
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err = os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path must be a directory")
	}
	return filepath.Clean(resolved), nil
}

func pathsOverlap(first, second string) bool {
	return pathContains(first, second) || pathContains(second, first)
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	if err != nil {
		return true
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
