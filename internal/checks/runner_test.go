package checks

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCommandRunnerBoundsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper executable uses a POSIX shebang")
	}
	root := prepareRunnerHelper(t, "output")
	profile := helperProfile(32, time.Second)
	result := (CommandRunner{}).Run(context.Background(), root, profile)
	if result.Passed || !result.Truncated || len(result.Output) != 32 {
		t.Fatalf("result = %#v", result)
	}
	if result.ErrorMessage != "check produced unexpected output" {
		t.Fatalf("error message = %q", result.ErrorMessage)
	}
}

func TestCommandRunnerEnforcesTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper executable uses a POSIX shebang")
	}
	root := prepareRunnerHelper(t, "sleep")
	result := (CommandRunner{}).Run(context.Background(), root, helperProfile(128, 30*time.Millisecond))
	if result.Passed || !result.TimedOut || result.ExitCode != -1 {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(result.ErrorMessage, "timed out") {
		t.Fatalf("error message = %q", result.ErrorMessage)
	}
}

func TestCommandRunnerHonorsParentCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper executable uses a POSIX shebang")
	}
	root := prepareRunnerHelper(t, "sleep")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	result := (CommandRunner{}).Run(ctx, root, helperProfile(128, time.Second))
	if result.Passed || !result.Cancelled {
		t.Fatalf("result = %#v", result)
	}
}

func TestCommandRunnerRejectsUnknownProfileBeforeExecution(t *testing.T) {
	result := (CommandRunner{}).Run(context.Background(), t.TempDir(), Profile{
		Name: "unsafe", Command: []string{"sh", "-c", "echo unsafe"},
		Timeout: time.Second, OutputLimit: 128,
	})
	if result.Passed || !strings.Contains(result.ErrorMessage, "unknown check profile") {
		t.Fatalf("result = %#v", result)
	}
}

func TestDockerArgsApplySandboxRestrictions(t *testing.T) {
	args := dockerArgs("/tmp/workspace", "orkoda:test", []string{"go", "test", "./..."})
	joined := strings.Join(args, " ")
	for _, required := range []string{"--network=none", "--read-only", "--cap-drop=ALL", "--pids-limit=128", "--memory=1g", "--cpus=2", "--tmpfs", "orkoda:test", "go test ./..."} {
		if !strings.Contains(joined, required) {
			t.Fatalf("Docker args missing %q: %v", required, args)
		}
	}
}

func prepareRunnerHelper(t *testing.T, mode string) string {
	t.Helper()
	root := t.TempDir()
	bin := t.TempDir()
	script := `#!/bin/sh
mode=$(cat .check-helper-mode)
case "$mode" in
  output)
    i=0
    while [ "$i" -lt 256 ]; do
      printf x
      i=$((i + 1))
    done
    ;;
  sleep)
    sleep 2
    ;;
  fail)
    echo failed
    exit 7
    ;;
esac
`
	path := filepath.Join(bin, "gofmt")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".check-helper-mode"), []byte(mode), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return root
}

func helperProfile(limit int, timeout time.Duration) Profile {
	return Profile{
		Name: "go.format", Command: []string{"gofmt", "-l", "main.go"},
		Timeout: timeout, OutputLimit: limit, RequireEmptyOutput: true,
	}
}
