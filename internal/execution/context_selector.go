package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultContextBytes = 64 * 1024
	defaultFileBytes    = 8 * 1024
	defaultContextFiles = 8
)

type ExecutionContext struct {
	Requirement        string            `json:"requirement"`
	AcceptanceCriteria []string          `json:"acceptance_criteria"`
	Constraints        []string          `json:"constraints"`
	RepositoryFiles    []string          `json:"repository_files"`
	SelectedFiles      map[string]string `json:"selected_files"`
	GitStatus          string            `json:"git_status"`
	Truncated          bool              `json:"truncated"`
}

type ContextSelector struct {
	db          *sql.DB
	maxBytes    int
	maxFileSize int
	maxFiles    int
}

func NewContextSelector(db *sql.DB) (*ContextSelector, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	return &ContextSelector{
		db: db, maxBytes: defaultContextBytes,
		maxFileSize: defaultFileBytes, maxFiles: defaultContextFiles,
	}, nil
}

func (s *ContextSelector) Select(ctx context.Context, execution Execution, root, gitStatus string) (ExecutionContext, error) {
	var requirement, criteriaJSON, constraintsJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT requirement, acceptance_criteria_json, constraints_json
		FROM plan_versions WHERE id = ?
	`, execution.PlanVersionID).Scan(&requirement, &criteriaJSON, &constraintsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return ExecutionContext{}, fmt.Errorf("%w: plan version is missing", ErrInvalid)
	}
	if err != nil {
		return ExecutionContext{}, fmt.Errorf("load execution plan context: %w", err)
	}

	result := ExecutionContext{
		Requirement: strings.TrimSpace(requirement), GitStatus: boundText(gitStatus, 8192),
		SelectedFiles: make(map[string]string),
	}
	if err := json.Unmarshal([]byte(criteriaJSON), &result.AcceptanceCriteria); err != nil {
		return ExecutionContext{}, fmt.Errorf("decode execution acceptance criteria: %w", err)
	}
	if err := json.Unmarshal([]byte(constraintsJSON), &result.Constraints); err != nil {
		return ExecutionContext{}, fmt.Errorf("decode execution constraints: %w", err)
	}

	files, err := collectContextFiles(root, 400)
	if err != nil {
		return ExecutionContext{}, err
	}
	result.RepositoryFiles = files
	remaining := s.maxBytes - len(result.Requirement) - len(criteriaJSON) - len(constraintsJSON) - len(result.GitStatus)
	if remaining < 0 {
		remaining = 0
		result.Truncated = true
	}

	for _, relative := range prioritizeContextFiles(files) {
		if len(result.SelectedFiles) >= s.maxFiles || remaining <= 0 {
			result.Truncated = true
			break
		}
		path := filepath.Join(root, filepath.FromSlash(relative))
		file, info, err := openRegularFile(path)
		if err != nil || info.Size() > int64(s.maxFileSize) {
			continue
		}
		content, err := io.ReadAll(io.LimitReader(file, int64(s.maxFileSize)+1))
		_ = file.Close()
		if err != nil {
			continue
		}
		if len(content) > s.maxFileSize {
			continue
		}
		value := string(content)
		if len(value) > remaining {
			value = value[:remaining]
			result.Truncated = true
		}
		result.SelectedFiles[relative] = value
		remaining -= len(value)
	}
	return result, nil
}

func collectContextFiles(root string, limit int) ([]string, error) {
	root = filepath.Clean(root)
	rootInfo, err := os.Lstat(root)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, fmt.Errorf("scan executor context files: unsafe workspace root")
	}
	files := make([]string, 0, min(limit, 128))
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			base := entry.Name()
			if base == ".git" || base == ".orkoda" || base == "node_modules" || base == "vendor" || base == "dist" || base == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= limit {
			return fs.SkipAll
		}
		if unsafeContextFile(relative) {
			return nil
		}
		files = append(files, relative)
		return nil
	})
	if err != nil && !errors.Is(err, fs.SkipAll) {
		return nil, fmt.Errorf("scan executor context files: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func prioritizeContextFiles(files []string) []string {
	items := append([]string(nil), files...)
	sort.SliceStable(items, func(i, j int) bool {
		return contextPriority(items[i]) < contextPriority(items[j])
	})
	return items
}

func contextPriority(path string) int {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "readme.md", "go.mod", "package.json", "bun.lock", "makefile":
		return 0
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx":
		return 1
	case ".md", ".json", ".toml", ".yaml", ".yml":
		return 2
	default:
		return 3
	}
}

func unsafeContextFile(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	base := strings.ToLower(filepath.Base(path))
	if strings.HasPrefix(base, ".env") || strings.Contains(base, "credential") ||
		strings.Contains(base, "secret") || strings.HasSuffix(base, ".pem") ||
		strings.HasSuffix(base, ".key") || strings.HasSuffix(base, ".p12") {
		return true
	}
	return strings.Contains(lower, "/.git/") || strings.HasPrefix(lower, ".git/")
}

func boundText(value string, limit int) string {
	if limit > 0 && len(value) > limit {
		return value[:limit]
	}
	return value
}
