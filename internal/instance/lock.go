package instance

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Lock struct {
	path string
	file *os.File
}

func Acquire(path string) (*Lock, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return nil, fmt.Errorf("instance lock path is required")
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("instance lock path must not be a symlink")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect instance lock: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create instance lock directory: %w", err)
	}
	file, err := openLockFile(path)
	if err != nil {
		return nil, fmt.Errorf("open instance lock: %w", err)
	}
	lock := &Lock{path: path, file: file}
	if err := lock.tryLock(); err != nil {
		file.Close()
		return nil, err
	}
	if err := file.Truncate(0); err == nil {
		_, err = file.WriteString(fmt.Sprintf("%d\n", os.Getpid()))
	}
	if err != nil {
		lock.Release()
		return nil, fmt.Errorf("write instance lock owner: %w", err)
	}
	return lock, nil
}

func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := l.unlock()
	var closeErr error
	if l.file != nil {
		closeErr = l.file.Close()
	}
	l.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
