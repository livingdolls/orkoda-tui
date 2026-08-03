//go:build windows

package checks

import (
	"context"
	"os"
	"os/exec"
)

func configureProcess(_ *exec.Cmd) {}

func terminateProcess(command *exec.Cmd) {
	if command.Process != nil {
		_ = command.Process.Kill()
	}
}

func runProcess(ctx context.Context, command *exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	configureProcess(command)
	command.Cancel = func() error {
		if command.Process == nil || command.ProcessState != nil {
			return os.ErrProcessDone
		}
		terminateProcess(command)
		return nil
	}
	return command.Run()
}
