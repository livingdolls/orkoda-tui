package execution

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/livingdolls/orkoda-tui/internal/agentconfig"
)

var (
	ErrUnsafePath     = errors.New("unsafe workspace path")
	ErrToolNotAllowed = errors.New("tool is not allowed")
	ErrSizeLimit      = errors.New("tool size limit exceeded")
)

type PathGuard struct{}

func (PathGuard) Resolve(root, requested string, allowMissing bool) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	requested = strings.TrimSpace(requested)
	if root == "" || requested == "" || strings.ContainsRune(requested, '\x00') || filepath.IsAbs(requested) {
		return "", ErrUnsafePath
	}
	clean := filepath.Clean(filepath.FromSlash(requested))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", ErrUnsafePath
	}
	if clean == ".git" || strings.HasPrefix(clean, ".git"+string(filepath.Separator)) {
		return "", ErrUnsafePath
	}
	candidate := filepath.Join(root, clean)
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrUnsafePath
	}
	if err := rejectSymlinkEscape(root, candidate, allowMissing); err != nil {
		return "", err
	}
	return candidate, nil
}

func rejectSymlinkEscape(root, candidate string, allowMissing bool) error {
	current := root
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return ErrUnsafePath
	}
	parts := strings.Split(relative, string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && allowMissing {
			if index < len(parts)-1 {
				continue
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect guarded path: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return ErrUnsafePath
		}
		if info.Mode()&(os.ModeDevice|os.ModeNamedPipe|os.ModeSocket) != 0 {
			return ErrUnsafePath
		}
	}
	return nil
}

type Toolset struct {
	Root   string
	Policy agentconfig.ToolPolicy
	Guard  PathGuard
}

func (t Toolset) Read(path string) (string, error) {
	if err := t.allow(agentconfig.ToolFileRead, false); err != nil {
		return "", err
	}
	resolved, err := t.Guard.Resolve(t.Root, path, false)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > int64(t.Policy.MaxFileBytes) {
		return "", ErrSizeLimit
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func (t Toolset) Search(query string, maxResults int) ([]string, error) {
	if err := t.allow(agentconfig.ToolFileSearch, false); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrInvalid
	}
	if maxResults < 1 || maxResults > 200 {
		maxResults = 50
	}
	matches := make([]string, 0)
	err := filepath.WalkDir(t.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == filepath.Join(t.Root, ".git") && entry.IsDir() {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > int64(t.Policy.MaxFileBytes) {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()
		scanner := bufio.NewScanner(io.LimitReader(file, int64(t.Policy.MaxFileBytes)))
		line := 0
		for scanner.Scan() {
			line++
			if strings.Contains(scanner.Text(), query) {
				relative, _ := filepath.Rel(t.Root, path)
				matches = append(matches, fmt.Sprintf("%s:%d", filepath.ToSlash(relative), line))
				if len(matches) >= maxResults {
					return io.EOF
				}
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return matches, nil
}

func (t Toolset) Create(path, content string) error {
	if err := t.allow(agentconfig.ToolFileCreate, true); err != nil {
		return err
	}
	if len([]byte(content)) > t.Policy.MaxFileBytes {
		return ErrSizeLimit
	}
	resolved, err := t.Guard.Resolve(t.Root, path, true)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(resolved); err == nil {
		return fmt.Errorf("file already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return err
	}
	return os.WriteFile(resolved, []byte(content), 0o644)
}

func (t Toolset) Patch(path, expected, replacement string) error {
	if err := t.allow(agentconfig.ToolFilePatch, true); err != nil {
		return err
	}
	if len([]byte(expected))+len([]byte(replacement)) > t.Policy.MaxPatchBytes {
		return ErrSizeLimit
	}
	resolved, err := t.Guard.Resolve(t.Root, path, false)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		return err
	}
	if len(content) > t.Policy.MaxFileBytes {
		return ErrSizeLimit
	}
	if bytes.Count(content, []byte(expected)) != 1 {
		return fmt.Errorf("patch expected text must occur exactly once")
	}
	updated := bytes.Replace(content, []byte(expected), []byte(replacement), 1)
	if len(updated) > t.Policy.MaxFileBytes {
		return ErrSizeLimit
	}
	temporary := resolved + ".orkoda-tmp"
	if err := os.WriteFile(temporary, updated, 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, resolved)
}

func (t Toolset) Delete(path string) error {
	if err := t.allow(agentconfig.ToolFileDelete, true); err != nil {
		return err
	}
	resolved, err := t.Guard.Resolve(t.Root, path, false)
	if err != nil {
		return err
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return ErrUnsafePath
	}
	return os.Remove(resolved)
}

func (t Toolset) GitStatus(ctx context.Context) (string, error) {
	if err := t.allow(agentconfig.ToolGitStatus, false); err != nil {
		return "", err
	}
	return t.git(ctx, "status", "--porcelain=v1", "--untracked-files=all")
}
func (t Toolset) GitDiff(ctx context.Context) (string, error) {
	if err := t.allow(agentconfig.ToolGitDiff, false); err != nil {
		return "", err
	}
	return t.git(ctx, "diff", "--binary", "--no-ext-diff", "--no-color")
}
func (t Toolset) Head(ctx context.Context) (string, error) {
	output, err := t.git(ctx, "rev-parse", "HEAD")
	return strings.TrimSpace(output), err
}
func (t Toolset) ChangedFiles(ctx context.Context) ([]string, error) {
	output, err := t.git(ctx, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return nil, err
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
		if path != "" {
			seen[path] = struct{}{}
		}
	}
	items := make([]string, 0, len(seen))
	for path := range seen {
		items = append(items, path)
	}
	sort.Strings(items)
	return items, nil
}

func (t Toolset) git(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", t.Root}, args...)...)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
	output, err := command.CombinedOutput()
	if len(output) > t.Policy.MaxPatchBytes && t.Policy.MaxPatchBytes > 0 {
		return "", ErrSizeLimit
	}
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func (t Toolset) allow(tool string, write bool) error {
	allowed := false
	for _, candidate := range t.Policy.AllowedTools {
		if candidate == tool {
			allowed = true
			break
		}
	}
	if !allowed {
		return ErrToolNotAllowed
	}
	if write && t.Policy.FilesystemAccess != agentconfig.FilesystemWorkspaceWrite {
		return ErrToolNotAllowed
	}
	return nil
}
