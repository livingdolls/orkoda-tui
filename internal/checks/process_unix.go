//go:build !windows

package checks

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

func configureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcess(command *exec.Cmd) {
	if command.Process == nil {
		return
	}
	// Kill the process group first so shell/tool descendants cannot outlive a
	// timed-out check. Fall back to the direct process when a group is gone.
	_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	_ = command.Process.Kill()
}

func runProcess(ctx context.Context, command *exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	configureProcess(command)
	// The command is created with CommandContext by the caller. Override the
	// default direct-child kill with a process-group kill so shell descendants
	// cannot survive. os/exec serializes this callback with Wait and the
	// ProcessState guard prevents killing a reused PID after normal exit.
	command.Cancel = func() error {
		if command.Process == nil || command.ProcessState != nil {
			return os.ErrProcessDone
		}
		terminateProcess(command)
		return nil
	}
	return command.Run()
}
