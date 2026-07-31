package repositorysummary

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestFileScannerDetectsStackAndSkipsUnsafePaths(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mustMkdir(t, filepath.Join(root, "src"))
	mustMkdir(t, filepath.Join(root, "node_modules", "ignored"))
	mustMkdir(t, filepath.Join(root, ".git"))
	mustWrite(t, filepath.Join(root, "src", "app.tsx"), "export const App = () => null")
	mustWrite(t, filepath.Join(root, "main.go"), "package main")
	mustWrite(t, filepath.Join(root, ".env"), "API_KEY=secret")
	mustWrite(t, filepath.Join(root, "node_modules", "ignored", "index.js"), "ignored")
	mustWrite(t, filepath.Join(outside, "outside.ts"), "should not be read")
	if err := os.Symlink(outside, filepath.Join(root, "outside-link")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	mustWrite(t, filepath.Join(root, "go.mod"), "module example\nrequire github.com/gin-gonic/gin v1.10.0\n")
	mustWrite(t, filepath.Join(root, "package.json"), `{
		"scripts":{"test":"bun test","lint":"biome check .","build":"bun build"},
		"dependencies":{"react":"latest","@opentui/react":"latest"},
		"devDependencies":{"typescript":"latest"}
	}`)
	mustWrite(t, filepath.Join(root, "bun.lock"), "")

	snapshot, err := NewScanner().Scan(context.Background(), root, "abc123")
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if snapshot.HeadSHA != "abc123" || snapshot.RootPath == "" {
		t.Fatalf("snapshot identity = %#v", snapshot)
	}
	for _, expected := range []string{"Go", "JavaScript", "TypeScript"} {
		if !slices.Contains(snapshot.Languages, expected) {
			t.Fatalf("languages = %#v, missing %q", snapshot.Languages, expected)
		}
	}
	for _, expected := range []string{"Gin", "OpenTUI", "React"} {
		if !slices.Contains(snapshot.Frameworks, expected) {
			t.Fatalf("frameworks = %#v, missing %q", snapshot.Frameworks, expected)
		}
	}
	if !slices.Contains(snapshot.PackageManagers, "Bun") || !slices.Contains(snapshot.PackageManagers, "Go Modules") {
		t.Fatalf("package managers = %#v", snapshot.PackageManagers)
	}
	if len(snapshot.Commands["test"]) == 0 || len(snapshot.Commands["lint"]) == 0 || len(snapshot.Commands["build"]) == 0 {
		t.Fatalf("commands = %#v", snapshot.Commands)
	}
	if slices.Contains(snapshot.ImportantFiles, ".env") {
		t.Fatalf("secret file leaked into important files: %#v", snapshot.ImportantFiles)
	}
	if slices.Contains(snapshot.TopLevelEntries, "node_modules") || slices.Contains(snapshot.TopLevelEntries, ".git") {
		t.Fatalf("ignored directory leaked into top level: %#v", snapshot.TopLevelEntries)
	}
	if snapshot.SkippedFiles < 3 {
		t.Fatalf("SkippedFiles = %d, want at least 3", snapshot.SkippedFiles)
	}
}

func TestFileScannerStopsAtConfiguredFileLimit(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 5; index++ {
		mustWrite(t, filepath.Join(root, "file-"+string(rune('a'+index))+".go"), "package example")
	}

	scanner := NewScanner()
	scanner.MaxFiles = 2
	snapshot, err := scanner.Scan(context.Background(), root, "head")
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if !snapshot.Truncated || snapshot.FileCount != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
