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
	Passed       bool
	ExitCode     int
	Duration     time.Duration
	Output       string
	OutputLimit  int
	Truncated    bool
	Cancelled    bool
	TimedOut     bool
	ErrorMessage string
}

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
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return invalid("workspace path is not a directory")
	}

	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	buffer := &limitedBuffer{limit: limit}
	command := exec.CommandContext(commandCtx, profile.Command[0], profile.Command[1:]...)
	command.Dir = root
	command.Stdout = buffer
	command.Stderr = buffer
	command.Env = constrainedEnvironment()
	command.WaitDelay = 2 * time.Second

	runErr := command.Run()
	result := Result{
		Passed: true, ExitCode: 0, Duration: time.Since(started), Output: buffer.String(),
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
	allowed := []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP", "GOCACHE", "GOMODCACHE"}
	environment := make([]string, 0, len(allowed)+9)
	for _, key := range allowed {
		if value := os.Getenv(key); value != "" {
			environment = append(environment, key+"="+value)
		}
	}
	return append(environment,
		"CI=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GONOSUMDB=*",
		"GOSUMDB=off",
		"GOPROXY=off",
		"npm_config_offline=true",
		"BUN_CONFIG_NO_NETWORK=1",
		"LC_ALL=C",
	)
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(payload []byte) (int, error) {
	original := len(payload)
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
