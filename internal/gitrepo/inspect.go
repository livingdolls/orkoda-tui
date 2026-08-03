package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Snapshot struct {
	RootPath      string `json:"root_path"`
	CurrentBranch string `json:"current_branch"`
	HeadSHA       string `json:"head_sha"`
	RemoteURL     string `json:"remote_url,omitempty"`
	Dirty         bool   `json:"dirty"`
}

type commandRunner interface {
	Run(context.Context, string, ...string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, directory string, arguments ...string) (string, error) {
	commandArguments := append([]string{"-C", directory}, arguments...)
	command := exec.CommandContext(ctx, "git", commandArguments...)
	command.Env = safeGitEnvironment()
	output, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			message := strings.TrimSpace(string(exitError.Stderr))
			if message != "" {
				return "", fmt.Errorf("git %s: %s", strings.Join(arguments, " "), message)
			}
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(arguments, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}

func safeGitEnvironment() []string {
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

type Inspector struct {
	runner commandRunner
}

func NewInspector() *Inspector {
	return &Inspector{runner: execRunner{}}
}

func newInspector(runner commandRunner) *Inspector {
	return &Inspector{runner: runner}
}

func (i *Inspector) Inspect(ctx context.Context, path string) (Snapshot, error) {
	if strings.TrimSpace(path) == "" {
		return Snapshot{}, fmt.Errorf("repository path is required")
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve repository path: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(absolutePath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve repository symlinks: %w", err)
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("inspect repository path: %w", err)
	}
	if !info.IsDir() {
		return Snapshot{}, fmt.Errorf("repository path must be a directory")
	}

	root, err := i.runner.Run(ctx, resolvedPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return Snapshot{}, fmt.Errorf("path is not a Git repository: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve Git root: %w", err)
	}

	headSHA, err := i.runner.Run(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return Snapshot{}, fmt.Errorf("read Git HEAD: %w", err)
	}
	branch, err := i.runner.Run(ctx, root, "branch", "--show-current")
	if err != nil {
		return Snapshot{}, fmt.Errorf("read current Git branch: %w", err)
	}
	status, err := i.runner.Run(ctx, root, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return Snapshot{}, fmt.Errorf("read Git working tree status: %w", err)
	}
	remoteURL, remoteErr := i.runner.Run(ctx, root, "remote", "get-url", "origin")
	if remoteErr != nil {
		remoteURL = ""
	}

	return Snapshot{
		RootPath:      filepath.Clean(root),
		CurrentBranch: branch,
		HeadSHA:       headSHA,
		RemoteURL:     remoteURL,
		Dirty:         status != "",
	}, nil
}
