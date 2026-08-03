package reviewer

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"github.com/livingdolls/orkoda-tui/internal/llm"
)

var issueKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

func ParseResponse(response llm.Response, validation ValidationContext) (Result, error) {
	decoder := json.NewDecoder(strings.NewReader(response.Content))
	decoder.DisallowUnknownFields()
	var result Result
	if err := decoder.Decode(&result); err != nil {
		return Result{}, invalidResponse(fmt.Sprintf("decode reviewer response: %v", err), err)
	}
	if err := ensureResponseEnd(decoder); err != nil {
		return Result{}, invalidResponse("reviewer response contains trailing content", err)
	}
	result = normalizeResult(result)
	if err := validateResult(result, validation); err != nil {
		return Result{}, invalidResponse(err.Error(), err)
	}
	return result, nil
}

func validateResult(result Result, validation ValidationContext) error {
	if result.Verdict != VerdictApprove && result.Verdict != VerdictRequestRevision {
		return fmt.Errorf("review verdict is invalid")
	}
	if result.Summary == "" {
		return fmt.Errorf("review summary is required")
	}
	if len(result.Summary) > 8000 {
		return fmt.Errorf("review summary exceeds 8000 bytes")
	}
	if len(result.Issues) > 100 {
		return fmt.Errorf("review cannot contain more than 100 issues")
	}
	seenKeys := make(map[string]struct{}, len(result.Issues))
	blocking := 0
	for index, issue := range result.Issues {
		if !issueKeyPattern.MatchString(issue.Key) {
			return fmt.Errorf("review issue %d key is invalid", index)
		}
		if _, exists := seenKeys[issue.Key]; exists {
			return fmt.Errorf("review issue key %q is duplicated", issue.Key)
		}
		seenKeys[issue.Key] = struct{}{}
		if !validSeverity(issue.Severity) {
			return fmt.Errorf("review issue %q severity is invalid", issue.Key)
		}
		if !validCategory(issue.Category) {
			return fmt.Errorf("review issue %q category is invalid", issue.Key)
		}
		if issue.Title == "" || issue.Description == "" {
			return fmt.Errorf("review issue %q title and description are required", issue.Key)
		}
		if len(issue.Title) > 500 || len(issue.Description) > 8000 {
			return fmt.Errorf("review issue %q text exceeds the allowed size", issue.Key)
		}
		if issue.Severity == SeverityCritical && !issue.Blocking {
			return fmt.Errorf("critical review issue %q must be blocking", issue.Key)
		}
		if issue.Blocking {
			blocking++
		}
		if err := validateIssueLocation(issue, validation.ChangedFiles); err != nil {
			return fmt.Errorf("review issue %q: %w", issue.Key, err)
		}
		seenCriteria := make(map[string]struct{}, len(issue.CriteriaRefs))
		for _, criterion := range issue.CriteriaRefs {
			if _, exists := validation.CriteriaRefs[criterion]; !exists {
				return fmt.Errorf("review issue %q references unknown criterion %q", issue.Key, criterion)
			}
			if _, duplicate := seenCriteria[criterion]; duplicate {
				return fmt.Errorf("review issue %q repeats criterion %q", issue.Key, criterion)
			}
			seenCriteria[criterion] = struct{}{}
		}
	}
	if result.Verdict == VerdictApprove && blocking > 0 {
		return fmt.Errorf("APPROVE cannot contain blocking issues")
	}
	if result.Verdict == VerdictRequestRevision && blocking == 0 {
		return fmt.Errorf("REQUEST_REVISION requires at least one blocking issue")
	}
	return nil
}

func validateIssueLocation(issue Issue, changedFiles map[string]struct{}) error {
	if issue.FilePath == "" {
		if issue.LineStart != 0 || issue.LineEnd != 0 {
			return fmt.Errorf("line range requires file_path")
		}
		return nil
	}
	if strings.ContainsRune(issue.FilePath, '\x00') || strings.HasPrefix(issue.FilePath, "/") {
		return fmt.Errorf("file_path must be repository-relative")
	}
	cleaned := path.Clean(strings.ReplaceAll(issue.FilePath, "\\", "/"))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("file_path escapes the repository root")
	}
	if _, exists := changedFiles[cleaned]; !exists {
		return fmt.Errorf("file_path %q is not in changed_files", cleaned)
	}
	if issue.LineStart < 0 || issue.LineEnd < 0 {
		return fmt.Errorf("line range cannot be negative")
	}
	if issue.LineEnd > 0 && issue.LineStart == 0 {
		return fmt.Errorf("line_end requires line_start")
	}
	if issue.LineEnd > 0 && issue.LineEnd < issue.LineStart {
		return fmt.Errorf("line_end cannot precede line_start")
	}
	return nil
}

func normalizeResult(result Result) Result {
	result.Summary = strings.TrimSpace(result.Summary)
	if result.Issues == nil {
		result.Issues = []Issue{}
	}
	for index := range result.Issues {
		issue := &result.Issues[index]
		issue.Key = strings.TrimSpace(issue.Key)
		issue.Title = strings.TrimSpace(issue.Title)
		issue.Description = strings.TrimSpace(issue.Description)
		issue.FilePath = strings.TrimSpace(issue.FilePath)
		if issue.FilePath != "" {
			issue.FilePath = path.Clean(strings.ReplaceAll(issue.FilePath, "\\", "/"))
		}
		issue.CriteriaRefs = normalizedStrings(issue.CriteriaRefs)
	}
	return result
}

func validSeverity(value Severity) bool {
	switch value {
	case SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow:
		return true
	default:
		return false
	}
}

func validCategory(value Category) bool {
	switch value {
	case CategoryCorrectness, CategorySecurity, CategoryReliability, CategoryPerformance,
		CategoryMaintainability, CategoryTesting, CategoryRequirement:
		return true
	default:
		return false
	}
}

func ensureResponseEnd(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("unexpected trailing JSON value")
}

func invalidResponse(message string, cause error) error {
	return &llm.ProviderError{
		Code:    llm.ErrorInvalidResponse,
		Message: message,
		Cause:   cause,
	}
}
