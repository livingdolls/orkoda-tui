package checks

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

const DefaultSandboxImage = "orkoda-sandbox:local"

// DockerRunner executes an allowlisted profile in a disposable, networkless
// container. The workspace is the only host path mounted into the container.
type DockerRunner struct {
	DockerBinary string
	Image        string
}

func NewDockerRunner(image string) DockerRunner {
	image = strings.TrimSpace(image)
	if image == "" {
		image = DefaultSandboxImage
	}
	binary := strings.TrimSpace(os.Getenv("ORKODA_DOCKER_BINARY"))
	if binary == "" {
		binary = "docker"
	}
	return DockerRunner{DockerBinary: binary, Image: image}
}

func (r DockerRunner) Run(ctx context.Context, root string, profile Profile) Result {
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
		return Result{Passed: false, ExitCode: -1, Duration: time.Since(started), OutputLimit: limit, ErrorMessage: message}
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
	if strings.TrimSpace(r.DockerBinary) == "" || strings.TrimSpace(r.Image) == "" {
		return invalid("Docker sandbox is not configured")
	}

	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	buffer := &limitedBuffer{limit: limit, artifactLimit: DefaultArtifactOutputLimit}
	command := exec.CommandContext(commandCtx, r.DockerBinary, dockerArgs(root, r.Image, profile.Command)...)
	command.Dir = root
	command.Stdout = buffer
	command.Stderr = buffer
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		pathEnv = "/usr/bin:/bin"
	}
	command.Env = []string{"PATH=" + pathEnv, "DOCKER_CONFIG=/tmp/orkoda-docker-config"}
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
	if commandCtx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ErrorMessage = fmt.Sprintf("check timed out after %s", timeout)
	}
	if commandCtx.Err() == context.Canceled {
		result.Cancelled = true
		result.ErrorMessage = "check cancelled"
	}
	return result
}

func dockerArgs(root, image string, command []string) []string {
	return append([]string{
		"run", "--rm", "--init",
		"--network=none",
		"--read-only",
		"--cap-drop=ALL",
		"--security-opt=no-new-privileges",
		"--pids-limit=128",
		"--memory=1g",
		"--cpus=2",
		"--ulimit", "fsize=2147483648:2147483648",
		"--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=64m",
		"--user", currentDockerIdentity(),
		"--mount", "type=bind,src=" + root + ",dst=/workspace,ro",
		"--mount", "type=volume,dst=/workspace/node_modules",
		"--workdir", "/workspace",
		"--env", "CI=1",
		"--env", "HOME=/tmp",
		"--env", "TMPDIR=/tmp",
		"--env", "GOPROXY=off",
		"--env", "GOSUMDB=off",
		"--env", "npm_config_offline=true",
		"--env", "BUN_CONFIG_NO_NETWORK=1",
		image,
	}, command...)
}
