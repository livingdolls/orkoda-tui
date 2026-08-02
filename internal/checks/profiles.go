package checks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	DefaultCommandTimeout = 2 * time.Minute
	DefaultOutputLimit    = 1024 * 1024
)

type Profile struct {
	Name               string        `json:"name"`
	Command            []string      `json:"command"`
	Timeout            time.Duration `json:"timeout"`
	OutputLimit        int           `json:"output_limit"`
	RequireEmptyOutput bool          `json:"require_empty_output"`
}

type ProfileDetector interface {
	Detect(string) ([]Profile, error)
}

type Detector struct {
	CommandTimeout time.Duration
	OutputLimit    int
	MaxGoFiles     int
}

func NewDetector() Detector {
	return Detector{
		CommandTimeout: DefaultCommandTimeout,
		OutputLimit:    DefaultOutputLimit,
		MaxGoFiles:     500,
	}
}

func (d Detector) Detect(root string) ([]Profile, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || !filepath.IsAbs(root) {
		return nil, fmt.Errorf("%w: absolute workspace path is required", ErrInvalid)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: workspace path is not a directory", ErrInvalid)
	}
	if d.CommandTimeout <= 0 {
		d.CommandTimeout = DefaultCommandTimeout
	}
	if d.OutputLimit <= 0 {
		d.OutputLimit = DefaultOutputLimit
	}
	if d.MaxGoFiles <= 0 {
		d.MaxGoFiles = 500
	}

	profiles := make([]Profile, 0, 8)
	if regularFile(filepath.Join(root, "go.mod")) {
		files, collectErr := collectGoFiles(root, d.MaxGoFiles)
		if collectErr != nil {
			return nil, collectErr
		}
		if len(files) > 0 {
			profiles = append(profiles, Profile{
				Name:               "go.format",
				Command:            append([]string{"gofmt", "-l"}, files...),
				Timeout:            d.CommandTimeout,
				OutputLimit:        d.OutputLimit,
				RequireEmptyOutput: true,
			})
		}
		profiles = append(profiles,
			Profile{Name: "go.vet", Command: []string{"go", "vet", "./..."}, Timeout: d.CommandTimeout, OutputLimit: d.OutputLimit},
			Profile{Name: "go.test", Command: []string{"go", "test", "./..."}, Timeout: d.CommandTimeout, OutputLimit: d.OutputLimit},
		)
	}

	bunProfiles, err := detectBunProfiles(root, d.CommandTimeout, d.OutputLimit)
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, bunProfiles...)
	for _, profile := range profiles {
		if err := validateProfile(profile); err != nil {
			return nil, err
		}
	}
	return profiles, nil
}

func detectBunProfiles(root string, timeout time.Duration, outputLimit int) ([]Profile, error) {
	path := filepath.Join(root, "package.json")
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read package.json: %w", err)
	}
	if len(payload) > 1024*1024 {
		return nil, fmt.Errorf("%w: package.json exceeds 1 MiB", ErrInvalid)
	}
	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return nil, fmt.Errorf("decode package.json: %w", err)
	}
	ordered := []struct {
		Script   string
		Profile  string
		Category string
	}{
		{Script: "lint:ts", Profile: "bun.lint-ts", Category: "lint"},
		{Script: "lint", Profile: "bun.lint", Category: "lint"},
		{Script: "typecheck", Profile: "bun.typecheck", Category: "typecheck"},
		{Script: "test:ts", Profile: "bun.test-ts", Category: "test"},
		{Script: "test", Profile: "bun.test", Category: "test"},
		{Script: "build", Profile: "bun.build", Category: "build"},
	}
	profiles := make([]Profile, 0, len(ordered))
	seenCategory := make(map[string]bool, len(ordered))
	for _, candidate := range ordered {
		if strings.TrimSpace(manifest.Scripts[candidate.Script]) == "" || seenCategory[candidate.Category] {
			continue
		}
		seenCategory[candidate.Category] = true
		profiles = append(profiles, Profile{
			Name:        candidate.Profile,
			Command:     []string{"bun", "run", candidate.Script},
			Timeout:     timeout,
			OutputLimit: outputLimit,
		})
	}
	return profiles, nil
}

func validateProfile(profile Profile) error {
	if profile.Timeout <= 0 || profile.OutputLimit <= 0 || len(profile.Command) == 0 {
		return fmt.Errorf("%w: incomplete check profile %q", ErrInvalid, profile.Name)
	}
	command := profile.Command
	switch profile.Name {
	case "go.format":
		if len(command) < 3 || command[0] != "gofmt" || command[1] != "-l" || !profile.RequireEmptyOutput {
			return fmt.Errorf("%w: invalid go.format command", ErrInvalid)
		}
		for _, path := range command[2:] {
			clean := filepath.Clean(filepath.FromSlash(path))
			if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || !strings.HasSuffix(clean, ".go") {
				return fmt.Errorf("%w: unsafe go.format path", ErrInvalid)
			}
		}
	case "go.vet":
		if !equalCommand(command, "go", "vet", "./...") {
			return fmt.Errorf("%w: invalid go.vet command", ErrInvalid)
		}
	case "go.test":
		if !equalCommand(command, "go", "test", "./...") {
			return fmt.Errorf("%w: invalid go.test command", ErrInvalid)
		}
	case "bun.lint-ts":
		if !equalCommand(command, "bun", "run", "lint:ts") {
			return fmt.Errorf("%w: invalid bun.lint-ts command", ErrInvalid)
		}
	case "bun.lint":
		if !equalCommand(command, "bun", "run", "lint") {
			return fmt.Errorf("%w: invalid bun.lint command", ErrInvalid)
		}
	case "bun.typecheck":
		if !equalCommand(command, "bun", "run", "typecheck") {
			return fmt.Errorf("%w: invalid bun.typecheck command", ErrInvalid)
		}
	case "bun.test-ts":
		if !equalCommand(command, "bun", "run", "test:ts") {
			return fmt.Errorf("%w: invalid bun.test-ts command", ErrInvalid)
		}
	case "bun.test":
		if !equalCommand(command, "bun", "run", "test") {
			return fmt.Errorf("%w: invalid bun.test command", ErrInvalid)
		}
	case "bun.build":
		if !equalCommand(command, "bun", "run", "build") {
			return fmt.Errorf("%w: invalid bun.build command", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown check profile %q", ErrInvalid, profile.Name)
	}
	return nil
}

func equalCommand(command []string, expected ...string) bool {
	if len(command) != len(expected) {
		return false
	}
	for index := range command {
		if command[index] != expected[index] {
			return false
		}
	}
	return true
}

func collectGoFiles(root string, limit int) ([]string, error) {
	files := make([]string, 0, 64)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".orkoda", "vendor", "node_modules", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= limit {
			return fs.SkipAll
		}
		if strings.HasSuffix(entry.Name(), ".go") {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.SkipAll) {
		return nil, fmt.Errorf("collect Go files: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
