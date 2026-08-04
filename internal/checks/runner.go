package checks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Result struct {
	Passed         bool
	ExitCode       int
	Duration       time.Duration
	Output         string
	ArtifactOutput string
	OutputLimit    int
	Truncated      bool
	Cancelled      bool
	TimedOut       bool
	ErrorMessage   string
}

const DefaultArtifactOutputLimit = 8 * 1024 * 1024

type Runner interface {
	Run(context.Context, string, Profile) Result
}

type CommandRunner struct{}

func (CommandRunner) Run(ctx context.Context, root string, profile Profile) Result {
	started := time.Now()
	limit := profile.OutputLimit
	if limit <= 0 {
		limit = DefaultOutputLimit
	}
	timeout := profile.Timeout
	if timeout <= 0 {
		timeout = DefaultCommandTimeout
	}
	invalid := func(message string) Result {
		return Result{
			Passed: false, ExitCode: -1, Duration: time.Since(started),
			OutputLimit: limit, ErrorMessage: message,
		}
	}
	if err := validateProfile(profile); err != nil {
		return invalid(err.Error())
	}
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || !filepath.IsAbs(root) {
		return invalid("absolute workspace path is required")
	}
	info, err := os.Lstat(root)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return invalid("workspace path is not a directory")
	}

	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	buffer := &limitedBuffer{limit: limit, artifactLimit: DefaultArtifactOutputLimit}
	command := exec.CommandContext(commandCtx, profile.Command[0], profile.Command[1:]...)
	command.Dir = root
	command.Stdout = buffer
	command.Stderr = buffer
	command.Env = constrainedEnvironment()
	command.WaitDelay = 2 * time.Second

	runErr := runProcess(commandCtx, command)
	result := Result{
		Passed: true, ExitCode: 0, Duration: time.Since(started), Output: buffer.String(), ArtifactOutput: buffer.ArtifactString(),
		OutputLimit: limit, Truncated: buffer.truncated,
	}
	if profile.RequireEmptyOutput && strings.TrimSpace(result.Output) != "" {
		result.Passed = false
		result.ExitCode = 1
		result.ErrorMessage = "check produced unexpected output"
	}
	if runErr == nil {
		return result
	}
	result.Passed = false
	result.ErrorMessage = bound(runErr.Error(), 1024)
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	} else {
		result.ExitCode = -1
	}
	if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
		result.TimedOut = true
		result.ErrorMessage = fmt.Sprintf("check timed out after %s", timeout)
	}
	if errors.Is(commandCtx.Err(), context.Canceled) {
		result.Cancelled = true
		result.ErrorMessage = "check cancelled"
	}
	return result
}

func constrainedEnvironment() []string {
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/bin:/bin"
	}
	return []string{
		"PATH=" + path,
		"HOME=/tmp/orkoda-home",
		"TMPDIR=/tmp",
		"TMP=/tmp",
		"TEMP=/tmp",
		"CI=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_OPTIONAL_LOCKS=0",
		"GONOSUMDB=*",
		"GOSUMDB=off",
		"GOPROXY=off",
		"npm_config_offline=true",
		"BUN_CONFIG_NO_NETWORK=1",
		"LC_ALL=C",
	}
}

type limitedBuffer struct {
	buffer        bytes.Buffer
	artifact      bytes.Buffer
	limit         int
	artifactLimit int
	truncated     bool
}

func (b *limitedBuffer) Write(payload []byte) (int, error) {
	original := len(payload)
	artifactRemaining := b.artifactLimit - b.artifact.Len()
	if artifactRemaining > 0 {
		artifactPayload := payload
		if len(artifactPayload) > artifactRemaining {
			artifactPayload = artifactPayload[:artifactRemaining]
		}
		_, _ = b.artifact.Write(artifactPayload)
	}
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return original, nil
	}
	if len(payload) > remaining {
		payload = payload[:remaining]
		b.truncated = true
	}
	_, _ = b.buffer.Write(payload)
	return original, nil
}

func (b *limitedBuffer) String() string { return b.buffer.String() }

func (b *limitedBuffer) ArtifactString() string { return b.artifact.String() }
