//go:build !windows

package instance

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func openLockFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
}

func (l *Lock) tryLock() error {
	if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return fmt.Errorf("another Orkoda daemon instance is already running")
		}
		return fmt.Errorf("acquire instance lock: %w", err)
	}
	return nil
}

func (l *Lock) unlock() error {
	if l == nil || l.file == nil {
		return nil
	}
	return syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
}
