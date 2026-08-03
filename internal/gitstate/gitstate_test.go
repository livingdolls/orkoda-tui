package gitstate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureBindsUntrackedFiles(t *testing.T) {
	root := t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.name", "Test")
	runGitTest(t, root, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "tracked.txt")
	runGitTest(t, root, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("new content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	snapshot, err := Capture(context.Background(), root, DefaultMaxPatchBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Dirty || len(snapshot.ChangedFiles) != 1 || snapshot.ChangedFiles[0] != "new.txt" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if !strings.Contains(snapshot.Patch, "new.txt") || !strings.Contains(snapshot.Patch, "new content") {
		t.Fatalf("untracked patch missing from snapshot: %q", snapshot.Patch)
	}

	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("changed content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := Capture(context.Background(), root, DefaultMaxPatchBytes)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Checksum == snapshot.Checksum {
		t.Fatal("checksum did not change after untracked file mutation")
	}
}

func runGitTest(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
