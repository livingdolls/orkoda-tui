package checks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	ErrorMessage string
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
	if len(profile.Command) == 0 || strings.TrimSpace(profile.Command[0]) == "" {
		return Result{Passed: false, ExitCode: -1, Duration: time.Since(started), OutputLimit: limit, ErrorMessage: "check command is empty"}
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

	err := command.Run()
	result := Result{
		Passed: true, ExitCode: 0, Duration: time.Since(started), Output: buffer.String(),
		OutputLimit: limit, Truncated: buffer.truncated,
	}
	if profile.RequireEmptyOutput && strings.TrimSpace(result.Output) != "" {
		result.Passed = false
		result.ExitCode = 1
		result.ErrorMessage = "check produced unexpected output"
	}
	if err == nil {
		return result
	}
	result.Passed = false
	result.ErrorMessage = bound(err.Error(), 1024)
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	} else {
		result.ExitCode = -1
	}
	if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
		result.ErrorMessage = fmt.Sprintf("check timed out after %s", timeout)
	}
	if errors.Is(commandCtx.Err(), context.Canceled) {
		result.ErrorMessage = "check cancelled"
	}
	return result
}

func constrainedEnvironment() []string {
	allowed := []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP", "GOCACHE", "GOMODCACHE"}
	environment := make([]string, 0, len(allowed)+8)
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
