package checks

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDetectorSelectsAllowlistedGoAndBunProfiles(t *testing.T) {
	root := t.TempDir()
	writeCheckFile(t, root, "go.mod", "module example.com/checks\n\ngo 1.26\n")
	writeCheckFile(t, root, "main.go", "package main\n")
	writeCheckFile(t, root, "vendor/ignored.go", "package ignored\n")
	writeCheckFile(t, root, "package.json", `{
  "scripts": {
    "lint:ts": "biome check .",
    "lint": "eslint .",
    "typecheck": "tsc --noEmit",
    "test:ts": "bun test",
    "test": "vitest",
    "build": "bun build src/index.ts",
    "dangerous": "curl example.com"
  }
}`)

	profiles, err := NewDetector().Detect(root)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	names := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		names = append(names, profile.Name)
	}
	want := []string{
		"go.format", "go.vet", "go.test", "bun.lint-ts",
		"bun.typecheck", "bun.test-ts", "bun.build",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("profile names = %v, want %v", names, want)
	}
	if slices.Contains(names, "dangerous") || slices.Contains(names, "bun.lint") || slices.Contains(names, "bun.test") {
		t.Fatalf("detector selected duplicate or unsafe profile: %v", names)
	}
	if got := profiles[0].Command; len(got) != 3 || got[2] != "main.go" {
		t.Fatalf("go.format command = %v", got)
	}
}

func TestValidateProfileRejectsArbitraryCommandsAndTraversal(t *testing.T) {
	invalid := []Profile{
		{Name: "command.run", Command: []string{"sh", "-c", "echo unsafe"}, OutputLimit: 1, Timeout: 1},
		{Name: "go.vet", Command: []string{"go", "env"}, OutputLimit: 1, Timeout: 1},
		{
			Name: "go.format", Command: []string{"gofmt", "-l", "../outside.go"},
			RequireEmptyOutput: true, OutputLimit: 1, Timeout: 1,
		},
	}
	for _, profile := range invalid {
		if err := validateProfile(profile); err == nil {
			t.Fatalf("validateProfile(%#v) unexpectedly succeeded", profile)
		}
	}
}

func writeCheckFile(t *testing.T, root string, relative string, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
