package instance

import (
	"strings"
	"testing"
)

func TestAcquireAllowsOnlyOneProcessLock(t *testing.T) {
	path := t.TempDir() + "/daemon.lock"
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	if _, err := Acquire(path); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second Acquire() error = %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	second.Release()
}
