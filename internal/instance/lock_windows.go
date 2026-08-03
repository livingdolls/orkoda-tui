//go:build windows

package instance

import (
	"fmt"
	"os"
)

func (l *Lock) tryLock() error {
	return nil
}

func openLockFile(path string) (*os.File, error) {
	// O_EXCL gives Windows users the same single-process guard. The marker is
	// removed on a normal shutdown; an interrupted process can be recovered by
	// deleting the stale lock after verifying no daemon is running.
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("another Orkoda daemon instance is already running")
	}
	return file, nil
}

func (l *Lock) unlock() error {
	if l == nil || l.file == nil {
		return nil
	}
	if err := l.file.Close(); err != nil {
		return err
	}
	l.file = nil
	return os.Remove(l.path)
}
