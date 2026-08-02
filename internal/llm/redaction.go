package llm

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type RedactionMode string

const (
	RedactionModeStrict RedactionMode = "strict"
	RedactionModeReport RedactionMode = "report"
	RedactionModeOff    RedactionMode = "off"
)

type RedactionReport struct {
	Mode  RedactionMode  `json:"mode"`
	Count int            `json:"count"`
	Types map[string]int `json:"types"`
}

type PromptRedactor interface {
	Redact(Request, RedactionMode) (Request, RedactionReport, error)
}

type redactionPattern struct {
	kind        string
	expression  *regexp.Regexp
	secretGroup int
}

type StandardRedactor struct {
	patterns []redactionPattern
}

func NewStandardRedactor() *StandardRedactor {
	return &StandardRedactor{patterns: []redactionPattern{
		{
			kind: "PRIVATE_KEY",
			expression: regexp.MustCompile(
				`(?s)-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----.*?-----END (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`,
			),
		},
		{
			kind:        "API_TOKEN",
			expression:  regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)([A-Za-z0-9._~+/=-]{12,})`),
			secretGroup: 2,
		},
		{
			kind:        "URL_PASSWORD",
			expression:  regexp.MustCompile(`(?i)(https?://[^/\s:@]+:)([^@\s/]+)(@)`),
			secretGroup: 2,
		},
		{
			kind:        "ENV_SECRET",
			expression:  regexp.MustCompile(`(?im)^(\s*[A-Z0-9_]*(?:PASSWORD|PASSWD|API_KEY|TOKEN|SECRET)[A-Z0-9_]*\s*=\s*)([^\s#]{6,})`),
			secretGroup: 2,
		},
		{
			kind:       "JWT",
			expression: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`),
		},
		{
			kind:       "API_TOKEN",
			expression: regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{16,}|ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|AKIA[0-9A-Z]{16})\b`),
		},
	}}
}

func (r *StandardRedactor) Redact(request Request, mode RedactionMode) (Request, RedactionReport, error) {
	if r == nil {
		return Request{}, RedactionReport{}, fmt.Errorf("prompt redactor is required")
	}
	if mode != RedactionModeStrict && mode != RedactionModeReport && mode != RedactionModeOff {
		return Request{}, RedactionReport{}, fmt.Errorf("unsupported prompt redaction mode %q", mode)
	}

	redacted := cloneRequest(request)
	report := RedactionReport{Mode: mode, Types: map[string]int{}}
	if mode == RedactionModeOff {
		return redacted, report, nil
	}

	apply := mode == RedactionModeStrict
	for index := range redacted.Messages {
		content := redacted.Messages[index].Content
		for _, pattern := range r.patterns {
			var count int
			content, count = applyRedactionPattern(content, pattern, apply)
			if count > 0 {
				report.Count += count
				report.Types[pattern.kind] += count
			}
		}
		redacted.Messages[index].Content = content
	}
	return redacted, report, nil
}

func applyRedactionPattern(input string, pattern redactionPattern, apply bool) (string, int) {
	matches := pattern.expression.FindAllStringSubmatchIndex(input, -1)
	if len(matches) == 0 {
		return input, 0
	}
	if !apply {
		return input, len(matches)
	}

	var result strings.Builder
	result.Grow(len(input))
	last := 0
	count := 0
	for _, match := range matches {
		secretStart, secretEnd := match[0], match[1]
		if pattern.secretGroup > 0 {
			position := pattern.secretGroup * 2
			if position+1 >= len(match) || match[position] < 0 || match[position+1] < 0 {
				continue
			}
			secretStart, secretEnd = match[position], match[position+1]
		}
		if secretStart < last {
			continue
		}
		secret := input[secretStart:secretEnd]
		result.WriteString(input[last:secretStart])
		result.WriteString(redactionPlaceholder(pattern.kind, secret))
		last = secretEnd
		count++
	}
	result.WriteString(input[last:])
	return result.String(), count
}

func redactionPlaceholder(kind string, secret string) string {
	digest := sha256.Sum256([]byte(secret))
	return fmt.Sprintf("[REDACTED:%s:%x]", kind, digest[:4])
}

func safeRedactionTypes(types map[string]int) map[string]int {
	if len(types) == 0 {
		return map[string]int{}
	}
	keys := make([]string, 0, len(types))
	for key := range types {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make(map[string]int, len(keys))
	for _, key := range keys {
		result[key] = types[key]
	}
	return result
}
