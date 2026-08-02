package checks

import (
	"encoding/json"
	"errors"
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

type Detector struct {
	CommandTimeout time.Duration
	OutputLimit    int
	MaxGoFiles     int
}

func NewDetector() Detector {
	return Detector{CommandTimeout: DefaultCommandTimeout, OutputLimit: DefaultOutputLimit, MaxGoFiles: 500}
}

func (d Detector) Detect(root string) ([]Profile, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" {
		return nil, ErrInvalid
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
		files, err := collectGoFiles(root, d.MaxGoFiles)
		if err != nil {
			return nil, err
		}
		if len(files) > 0 {
			profiles = append(profiles, Profile{
				Name: "go.format", Command: append([]string{"gofmt", "-l"}, files...),
				Timeout: d.CommandTimeout, OutputLimit: d.OutputLimit, RequireEmptyOutput: true,
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
	return profiles, nil
}

func detectBunProfiles(root string, timeout time.Duration, outputLimit int) ([]Profile, error) {
	path := filepath.Join(root, "package.json")
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return nil, err
	}
	ordered := []struct {
		Script  string
		Profile string
	}{
		{"lint:ts", "bun.lint-ts"},
		{"lint", "bun.lint"},
		{"typecheck", "bun.typecheck"},
		{"test:ts", "bun.test-ts"},
		{"test", "bun.test"},
		{"build", "bun.build"},
	}
	profiles := make([]Profile, 0, len(ordered))
	seenCategory := map[string]bool{}
	for _, candidate := range ordered {
		if strings.TrimSpace(manifest.Scripts[candidate.Script]) == "" {
			continue
		}
		category := strings.Split(candidate.Profile, ".")[1]
		if strings.HasPrefix(category, "lint") {
			category = "lint"
		}
		if strings.HasPrefix(category, "test") {
			category = "test"
		}
		if seenCategory[category] {
			continue
		}
		seenCategory[category] = true
		profiles = append(profiles, Profile{
			Name: candidate.Profile, Command: []string{"bun", "run", candidate.Script},
			Timeout: timeout, OutputLimit: outputLimit,
		})
	}
	return profiles, nil
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
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
