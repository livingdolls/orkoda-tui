package repositorysummary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultMaxFiles    = 5000
	defaultMaxReadSize = int64(256 * 1024)
	defaultMaxImportant = 100
)

var errFileLimitReached = errors.New("repository scan file limit reached")

type Commands map[string][]string

type Snapshot struct {
	RootPath        string   `json:"root_path"`
	HeadSHA         string   `json:"head_sha"`
	Languages       []string `json:"languages"`
	Frameworks      []string `json:"frameworks"`
	PackageManagers []string `json:"package_managers"`
	Commands        Commands `json:"commands"`
	ImportantFiles  []string `json:"important_files"`
	TopLevelEntries []string `json:"top_level_entries"`
	FileCount       int      `json:"file_count"`
	SkippedFiles    int      `json:"skipped_files"`
	Truncated       bool     `json:"truncated"`
}

type Scanner interface {
	Scan(context.Context, string, string) (Snapshot, error)
}

type FileScanner struct {
	MaxFiles     int
	MaxReadSize  int64
	MaxImportant int
}

func NewScanner() *FileScanner {
	return &FileScanner{
		MaxFiles:     defaultMaxFiles,
		MaxReadSize:  defaultMaxReadSize,
		MaxImportant: defaultMaxImportant,
	}
}

func (s *FileScanner) Scan(ctx context.Context, rootPath, headSHA string) (Snapshot, error) {
	rootPath = strings.TrimSpace(rootPath)
	headSHA = strings.TrimSpace(headSHA)
	if rootPath == "" || headSHA == "" {
		return Snapshot{}, fmt.Errorf("repository root and HEAD SHA are required")
	}

	absoluteRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve repository root: %w", err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve repository symlinks: %w", err)
	}
	info, err := os.Stat(canonicalRoot)
	if err != nil {
		return Snapshot{}, fmt.Errorf("stat repository root: %w", err)
	}
	if !info.IsDir() {
		return Snapshot{}, fmt.Errorf("repository root is not a directory")
	}

	maxFiles := s.MaxFiles
	if maxFiles < 1 {
		maxFiles = defaultMaxFiles
	}
	maxReadSize := s.MaxReadSize
	if maxReadSize < 1 {
		maxReadSize = defaultMaxReadSize
	}
	maxImportant := s.MaxImportant
	if maxImportant < 1 {
		maxImportant = defaultMaxImportant
	}

	languages := stringSet{}
	frameworks := stringSet{}
	packageManagers := stringSet{}
	topLevel := stringSet{}
	important := stringSet{}
	commands := Commands{}
	fileCount := 0
	skippedFiles := 0
	truncated := false

	walkErr := filepath.WalkDir(canonicalRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			skippedFiles++
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		relativePath, err := filepath.Rel(canonicalRoot, path)
		if err != nil {
			return fmt.Errorf("relativize repository path: %w", err)
		}
		if relativePath == "." {
			return nil
		}
		relativePath = filepath.ToSlash(relativePath)

		if entry.Type()&os.ModeSymlink != 0 {
			skippedFiles++
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if entry.IsDir() {
			if shouldIgnoreDirectory(entry.Name()) {
				skippedFiles++
				return filepath.SkipDir
			}
			if !strings.Contains(relativePath, "/") {
				topLevel.Add(relativePath)
			}
			return nil
		}

		if fileCount >= maxFiles {
			truncated = true
			return errFileLimitReached
		}
		fileCount++

		if shouldSkipSecret(relativePath) {
			skippedFiles++
			return nil
		}

		if !strings.Contains(relativePath, "/") {
			topLevel.Add(relativePath)
		}
		detectLanguage(relativePath, languages)
		if isImportantFile(relativePath) && important.Len() < maxImportant {
			important.Add(relativePath)
		}

		fileInfo, err := entry.Info()
		if err != nil {
			skippedFiles++
			return nil
		}
		if fileInfo.Size() > maxReadSize || !shouldReadForMetadata(relativePath) {
			if fileInfo.Size() > maxReadSize && shouldReadForMetadata(relativePath) {
				skippedFiles++
			}
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			skippedFiles++
			return nil
		}
		detectManifest(relativePath, content, languages, frameworks, packageManagers, commands)
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errFileLimitReached) {
		return Snapshot{}, fmt.Errorf("walk repository: %w", walkErr)
	}

	detectRootPackageManagers(canonicalRoot, packageManagers)
	normalizeCommands(commands)

	return Snapshot{
		RootPath:        canonicalRoot,
		HeadSHA:         headSHA,
		Languages:       languages.Sorted(),
		Frameworks:      frameworks.Sorted(),
		PackageManagers: packageManagers.Sorted(),
		Commands:        commands,
		ImportantFiles:  important.Sorted(),
		TopLevelEntries: topLevel.Sorted(),
		FileCount:       fileCount,
		SkippedFiles:    skippedFiles,
		Truncated:       truncated,
	}, nil
}

func shouldIgnoreDirectory(name string) bool {
	switch strings.ToLower(name) {
	case ".git", ".next", ".cache", ".idea", ".gradle", ".terraform", "node_modules", "vendor", "dist", "build", "coverage", "target", "out", "tmp", "temp":
		return true
	default:
		return false
	}
}

func shouldSkipSecret(relativePath string) bool {
	name := strings.ToLower(filepath.Base(relativePath))
	if name == ".env" || strings.HasPrefix(name, ".env.") || name == "id_rsa" || name == "id_ed25519" {
		return true
	}
	if strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key") || strings.HasSuffix(name, ".p12") || strings.HasSuffix(name, ".pfx") {
		return true
	}
	return strings.Contains(name, "credential") || strings.Contains(name, "secret")
}

func detectLanguage(path string, languages stringSet) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		languages.Add("Go")
	case ".ts", ".tsx":
		languages.Add("TypeScript")
	case ".js", ".jsx", ".mjs", ".cjs":
		languages.Add("JavaScript")
	case ".kt", ".kts":
		languages.Add("Kotlin")
	case ".java":
		languages.Add("Java")
	case ".rs":
		languages.Add("Rust")
	case ".py":
		languages.Add("Python")
	case ".php":
		languages.Add("PHP")
	case ".rb":
		languages.Add("Ruby")
	case ".sql":
		languages.Add("SQL")
	case ".swift":
		languages.Add("Swift")
	case ".dart":
		languages.Add("Dart")
	}
}

func isImportantFile(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	switch name {
	case "go.mod", "go.work", "package.json", "bun.lock", "bun.lockb", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "cargo.toml", "pyproject.toml", "requirements.txt", "composer.json", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "readme.md", "makefile", "dockerfile", ".gitignore", ".orkodaignore":
		return true
	default:
		return strings.HasSuffix(name, ".config.ts") || strings.HasSuffix(name, ".config.js")
	}
}

func shouldReadForMetadata(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	switch name {
	case "go.mod", "package.json", "cargo.toml", "pyproject.toml", "requirements.txt", "composer.json", "build.gradle", "build.gradle.kts", "makefile":
		return true
	default:
		return false
	}
}

func detectManifest(path string, content []byte, languages, frameworks, packageManagers stringSet, commands Commands) {
	name := strings.ToLower(filepath.Base(path))
	text := strings.ToLower(string(content))

	switch name {
	case "go.mod":
		languages.Add("Go")
		packageManagers.Add("Go Modules")
		detectGoFrameworks(text, frameworks)
	case "package.json":
		packageManagers.Add("npm-compatible")
		detectPackageJSON(content, languages, frameworks, commands)
	case "cargo.toml":
		languages.Add("Rust")
		packageManagers.Add("Cargo")
	case "pyproject.toml":
		languages.Add("Python")
		packageManagers.Add("Python packaging")
	case "requirements.txt":
		languages.Add("Python")
		packageManagers.Add("pip")
	case "composer.json":
		languages.Add("PHP")
		packageManagers.Add("Composer")
	case "build.gradle", "build.gradle.kts":
		packageManagers.Add("Gradle")
		if strings.Contains(text, "kotlin") {
			languages.Add("Kotlin")
		}
		if strings.Contains(text, "com.android.application") {
			frameworks.Add("Android")
		}
		if strings.Contains(text, "compose") {
			frameworks.Add("Jetpack Compose")
		}
	case "makefile":
		detectMakeCommands(string(content), commands)
	}
}

func detectGoFrameworks(text string, frameworks stringSet) {
	candidates := map[string]string{
		"github.com/gin-gonic/gin":       "Gin",
		"github.com/labstack/echo":       "Echo",
		"github.com/gofiber/fiber":       "Fiber",
		"github.com/spf13/cobra":         "Cobra",
		"github.com/charmbracelet/bubbletea": "Bubble Tea",
	}
	for dependency, framework := range candidates {
		if strings.Contains(text, dependency) {
			frameworks.Add(framework)
		}
	}
}

func detectPackageJSON(content []byte, languages, frameworks stringSet, commands Commands) {
	var manifest struct {
		Scripts      map[string]string `json:"scripts"`
		Dependencies map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return
	}

	languages.Add("JavaScript")
	for dependency := range manifest.Dependencies {
		detectJSFramework(dependency, frameworks)
	}
	for dependency := range manifest.DevDependencies {
		detectJSFramework(dependency, frameworks)
		if dependency == "typescript" {
			languages.Add("TypeScript")
		}
	}

	for name, command := range manifest.Scripts {
		category := commandCategory(name)
		if category != "" && strings.TrimSpace(command) != "" {
			commands[category] = append(commands[category], "bun run "+name)
		}
	}
}

func detectJSFramework(dependency string, frameworks stringSet) {
	switch strings.ToLower(dependency) {
	case "react", "react-dom":
		frameworks.Add("React")
	case "next":
		frameworks.Add("Next.js")
	case "@opentui/react", "@opentui/core":
		frameworks.Add("OpenTUI")
	case "express":
		frameworks.Add("Express")
	case "@nestjs/core":
		frameworks.Add("NestJS")
	case "vite":
		frameworks.Add("Vite")
	case "vue":
		frameworks.Add("Vue")
	case "svelte":
		frameworks.Add("Svelte")
	}
}

func commandCategory(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "test"):
		return "test"
	case strings.Contains(lower, "lint"):
		return "lint"
	case strings.Contains(lower, "format") || lower == "fmt":
		return "format"
	case strings.Contains(lower, "build"):
		return "build"
	default:
		return ""
	}
}

func detectMakeCommands(content string, commands Commands) {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, " ") || !strings.Contains(line, ":") {
			continue
		}
		target := strings.TrimSpace(strings.SplitN(line, ":", 2)[0])
		category := commandCategory(target)
		if category != "" && target != "" {
			commands[category] = append(commands[category], "make "+target)
		}
	}
}

func detectRootPackageManagers(root string, packageManagers stringSet) {
	candidates := map[string]string{
		"bun.lock":          "Bun",
		"bun.lockb":         "Bun",
		"pnpm-lock.yaml":    "pnpm",
		"yarn.lock":         "Yarn",
		"package-lock.json": "npm",
	}
	for file, manager := range candidates {
		if _, err := os.Stat(filepath.Join(root, file)); err == nil {
			packageManagers.Add(manager)
		}
	}
}

func normalizeCommands(commands Commands) {
	for category, values := range commands {
		set := stringSet{}
		for _, value := range values {
			set.Add(value)
		}
		commands[category] = set.Sorted()
	}
}

type stringSet map[string]struct{}

func (s stringSet) Add(value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		s[value] = struct{}{}
	}
}

func (s stringSet) Len() int {
	return len(s)
}

func (s stringSet) Sorted() []string {
	values := make([]string, 0, len(s))
	for value := range s {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}
