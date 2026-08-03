// Package gitstate contains bounded, deterministic Git workspace inspection
// helpers used by workflow stages that need to bind a decision to a tree.
package gitstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const DefaultMaxPatchBytes = 32 * 1024 * 1024

var ErrPatchTooLarge = errors.New("git patch exceeds size limit")

type Snapshot struct {
	Head         string
	ChangedFiles []string
	Patch        string
	Checksum     string
	Dirty        bool
}

func Capture(ctx context.Context, root string, maxPatchBytes int) (Snapshot, error) {
	root, err := cleanRoot(root)
	if err != nil {
		return Snapshot{}, err
	}
	if maxPatchBytes <= 0 {
		maxPatchBytes = DefaultMaxPatchBytes
	}

	head, err := Run(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return Snapshot{}, fmt.Errorf("read workspace HEAD: %w", err)
	}
	changed, err := changedFiles(ctx, root)
	if err != nil {
		return Snapshot{}, err
	}
	untracked, err := untrackedFiles(ctx, root)
	if err != nil {
		return Snapshot{}, err
	}
	tracked, err := Run(ctx, root, "diff", "HEAD", "--binary", "--no-ext-diff", "--no-color")
	if err != nil {
		return Snapshot{}, fmt.Errorf("read tracked workspace diff: %w", err)
	}

	var patch strings.Builder
	patch.WriteString(tracked)
	for _, path := range untracked {
		untrackedPatch, runErr := runAllowExitOne(ctx, root, "diff", "--no-index", "--binary", "--no-ext-diff", "--no-color", os.DevNull, filepath.FromSlash(path))
		if runErr != nil {
			return Snapshot{}, fmt.Errorf("read untracked workspace diff %q: %w", path, runErr)
		}
		patch.WriteString(untrackedPatch)
	}
	patchText := patch.String()
	if len([]byte(patchText)) > maxPatchBytes {
		return Snapshot{}, ErrPatchTooLarge
	}
	checksum := Checksum(patchText)
	return Snapshot{
		Head:         strings.TrimSpace(head),
		ChangedFiles: changed,
		Patch:        patchText,
		Checksum:     checksum,
		Dirty:        len(changed) > 0,
	}, nil
}

// Checksum returns the stable content binding used by approvals and
// publication. It is exported so a recovered Git commit can be compared with
// the exact patch that was approved.
func Checksum(patch string) string {
	hash := sha256.Sum256([]byte(patch))
	return "sha256:" + hex.EncodeToString(hash[:])
}

func Run(ctx context.Context, root string, args ...string) (string, error) {
	root, err := cleanRoot(root)
	if err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	command.Env = safeEnvironment()
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func RunAllowExit(ctx context.Context, root string, acceptedExitCode int, args ...string) (string, error) {
	root, err := cleanRoot(root)
	if err != nil {
		return "", err
	}
	return runAllowExitCode(ctx, root, acceptedExitCode, args...)
}

func changedFiles(ctx context.Context, root string) ([]string, error) {
	output, err := Run(ctx, root, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return nil, fmt.Errorf("read workspace status: %w", err)
	}
	seen := map[string]struct{}{}
	for _, line := range strings.Split(output, "\n") {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if arrow := strings.LastIndex(path, " -> "); arrow >= 0 {
			path = path[arrow+4:]
		}
		if path == "" || filepath.IsAbs(filepath.FromSlash(path)) || filepath.Clean(filepath.FromSlash(path)) == ".." || strings.HasPrefix(filepath.Clean(filepath.FromSlash(path)), ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("unsafe changed workspace path %q", path)
		}
		seen[filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))] = struct{}{}
	}
	items := make([]string, 0, len(seen))
	for path := range seen {
		items = append(items, path)
	}
	sort.Strings(items)
	return items, nil
}

func untrackedFiles(ctx context.Context, root string) ([]string, error) {
	output, err := Run(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("read untracked workspace files: %w", err)
	}
	items := make([]string, 0)
	for _, raw := range strings.Split(output, "\x00") {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		clean := filepath.Clean(filepath.FromSlash(path))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("unsafe untracked workspace path %q", path)
		}
		items = append(items, filepath.ToSlash(clean))
	}
	sort.Strings(items)
	return items, nil
}

func runAllowExitOne(ctx context.Context, root string, args ...string) (string, error) {
	return runAllowExitCode(ctx, root, 1, args...)
}

func runAllowExitCode(ctx context.Context, root string, acceptedExitCode int, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	command.Env = safeEnvironment()
	output, err := command.CombinedOutput()
	if err == nil {
		return string(output), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == acceptedExitCode {
		return string(output), nil
	}
	return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
}

func cleanRoot(root string) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || !filepath.IsAbs(root) {
		return "", fmt.Errorf("absolute Git workspace path is required")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", fmt.Errorf("inspect Git workspace: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("Git workspace path is not a directory")
	}
	return root, nil
}

func safeEnvironment() []string {
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/bin:/bin"
	}
	return []string{
		"PATH=" + path,
		"HOME=/tmp/orkoda-home",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"LC_ALL=C",
	}
}
