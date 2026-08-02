package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type ValidationIssue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type StructuredValidator interface {
	Validate(json.RawMessage, string) (json.RawMessage, []ValidationIssue)
}

type JSONSchemaValidator struct{}

func (JSONSchemaValidator) Validate(schema json.RawMessage, content string) (json.RawMessage, []ValidationIssue) {
	normalizedContent, issue := normalizeStructuredContent(content)
	if issue != nil {
		return nil, []ValidationIssue{*issue}
	}

	decoder := json.NewDecoder(strings.NewReader(normalizedContent))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, []ValidationIssue{{Path: "$", Code: "invalid_json", Message: "response must contain valid JSON"}}
	}
	if err := ensureStructuredJSONEnd(decoder); err != nil {
		return nil, []ValidationIssue{{Path: "$", Code: "trailing_content", Message: "response must contain exactly one JSON value"}}
	}

	if len(bytes.TrimSpace(schema)) > 0 {
		var schemaValue map[string]any
		schemaDecoder := json.NewDecoder(bytes.NewReader(schema))
		schemaDecoder.UseNumber()
		if err := schemaDecoder.Decode(&schemaValue); err != nil {
			return nil, []ValidationIssue{{Path: "$schema", Code: "invalid_schema", Message: "configured response schema is invalid"}}
		}
		issues := make([]ValidationIssue, 0)
		validateSchemaNode("$", value, schemaValue, &issues)
		if len(issues) > 0 {
			return nil, issues
		}
	}

	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, []ValidationIssue{{Path: "$", Code: "encoding_failed", Message: "validated response could not be normalized"}}
	}
	return canonical, nil
}

func normalizeStructuredContent(content string) (string, *ValidationIssue) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", &ValidationIssue{Path: "$", Code: "empty_response", Message: "response content is required"}
	}
	if !strings.HasPrefix(content, "```") {
		return content, nil
	}

	lines := strings.Split(content, "\n")
	if len(lines) < 3 {
		return "", &ValidationIssue{Path: "$", Code: "invalid_code_fence", Message: "structured response code fence is incomplete"}
	}
	opening := strings.TrimSpace(lines[0])
	if opening != "```" && !strings.EqualFold(opening, "```json") {
		return "", &ValidationIssue{Path: "$", Code: "invalid_code_fence", Message: "only a single JSON code fence can be normalized"}
	}
	if strings.TrimSpace(lines[len(lines)-1]) != "```" {
		return "", &ValidationIssue{Path: "$", Code: "invalid_code_fence", Message: "structured response code fence is incomplete"}
	}
	inner := strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
	if inner == "" {
		return "", &ValidationIssue{Path: "$", Code: "empty_response", Message: "response content is required"}
	}
	return inner, nil
}

func ensureStructuredJSONEnd(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("unexpected trailing JSON value")
}

func validateSchemaNode(path string, value any, schema map[string]any, issues *[]ValidationIssue) {
	if len(*issues) >= 20 {
		return
	}
	typeName, _ := schema["type"].(string)
	if typeName != "" && !matchesSchemaType(typeName, value) {
		appendValidationIssue(issues, path, "type", fmt.Sprintf("must be %s", typeName))
		return
	}

	switch typeName {
	case "object":
		validateSchemaObject(path, value, schema, issues)
	case "array":
		validateSchemaArray(path, value, schema, issues)
	case "string":
		validateSchemaString(path, value, schema, issues)
	}
}

func validateSchemaObject(path string, value any, schema map[string]any, issues *[]ValidationIssue) {
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	properties, _ := schema["properties"].(map[string]any)
	if required, ok := schema["required"].([]any); ok {
		for _, item := range required {
			name, ok := item.(string)
			if !ok {
				continue
			}
			if _, exists := object[name]; !exists {
				appendValidationIssue(issues, joinJSONPath(path, name), "required", "is required")
			}
		}
	}
	if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
		for name := range object {
			if _, exists := properties[name]; !exists {
				appendValidationIssue(issues, joinJSONPath(path, name), "additional_property", "is not allowed")
			}
		}
	}
	for name, childValue := range object {
		rawChild, exists := properties[name]
		if !exists {
			continue
		}
		childSchema, ok := rawChild.(map[string]any)
		if !ok {
			continue
		}
		validateSchemaNode(joinJSONPath(path, name), childValue, childSchema, issues)
	}
}

func validateSchemaArray(path string, value any, schema map[string]any, issues *[]ValidationIssue) {
	array, ok := value.([]any)
	if !ok {
		return
	}
	if minimum, ok := schemaInteger(schema["minItems"]); ok && len(array) < minimum {
		appendValidationIssue(issues, path, "min_items", fmt.Sprintf("must contain at least %d item(s)", minimum))
	}
	if maximum, ok := schemaInteger(schema["maxItems"]); ok && len(array) > maximum {
		appendValidationIssue(issues, path, "max_items", fmt.Sprintf("must contain at most %d item(s)", maximum))
	}
	itemSchema, _ := schema["items"].(map[string]any)
	for index, item := range array {
		if len(*issues) >= 20 {
			return
		}
		validateSchemaNode(fmt.Sprintf("%s[%d]", path, index), item, itemSchema, issues)
	}
}

func validateSchemaString(path string, value any, schema map[string]any, issues *[]ValidationIssue) {
	text, ok := value.(string)
	if !ok {
		return
	}
	if minimum, ok := schemaInteger(schema["minLength"]); ok && len([]rune(strings.TrimSpace(text))) < minimum {
		appendValidationIssue(issues, path, "min_length", fmt.Sprintf("must contain at least %d character(s)", minimum))
	}
	if maximum, ok := schemaInteger(schema["maxLength"]); ok && len([]rune(text)) > maximum {
		appendValidationIssue(issues, path, "max_length", fmt.Sprintf("must contain at most %d character(s)", maximum))
	}
}

func matchesSchemaType(typeName string, value any) bool {
	switch typeName {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(json.Number)
		return ok
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return false
		}
		_, err := number.Int64()
		return err == nil
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	default:
		return true
	}
}

func schemaInteger(value any) (int, bool) {
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Int64()
		return int(parsed), err == nil && parsed >= 0
	case float64:
		return int(number), number >= 0 && number == float64(int(number))
	case int:
		return number, number >= 0
	default:
		return 0, false
	}
}

func joinJSONPath(parent string, name string) string {
	if parent == "$" {
		return "$." + name
	}
	return parent + "." + name
}

func appendValidationIssue(issues *[]ValidationIssue, path string, code string, message string) {
	if len(*issues) >= 20 {
		return
	}
	*issues = append(*issues, ValidationIssue{Path: path, Code: code, Message: message})
}
