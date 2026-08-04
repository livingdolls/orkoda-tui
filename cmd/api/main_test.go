package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenUnixListenerRestrictsAndReplacesStaleSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime", "orkoda.sock")
	stale, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("Unix sockets are unavailable: %v", err)
	}
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	listener, err := openUnixListener(path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("listener path mode = %v, want socket", info.Mode())
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestOpenUnixListenerRefusesRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orkoda.sock")
	if err := os.WriteFile(path, []byte("do not replace"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openUnixListener(path); err == nil {
		t.Fatal("openUnixListener() replaced a regular file")
	}
}
