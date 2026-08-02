package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

var validationTestSchema = json.RawMessage(`{
  "type":"object",
  "additionalProperties":false,
  "required":["summary","steps"],
  "properties":{
    "summary":{"type":"string","minLength":1},
    "steps":{"type":"array","minItems":1,"items":{"type":"string","minLength":1}}
  }
}`)

func TestJSONSchemaValidatorAcceptsAndNormalizesJSONFence(t *testing.T) {
	validator := JSONSchemaValidator{}
	normalized, issues := validator.Validate(validationTestSchema, "```json\n{\n  \"summary\": \"ok\", \"steps\": [\"one\"]\n}\n```")
	if len(issues) != 0 {
		t.Fatalf("unexpected validation issues: %#v", issues)
	}
	if string(normalized) != `{"steps":["one"],"summary":"ok"}` && string(normalized) != `{"summary":"ok","steps":["one"]}` {
		t.Fatalf("unexpected canonical JSON: %s", normalized)
	}
}

func TestJSONSchemaValidatorRejectsMalformedSchemaMismatchAndExtraProse(t *testing.T) {
	validator := JSONSchemaValidator{}

	_, issues := validator.Validate(validationTestSchema, `{"summary":`)
	assertValidationCode(t, issues, "invalid_json")

	_, issues = validator.Validate(validationTestSchema, `{"summary":1,"steps":[]}`)
	if len(issues) < 2 {
		t.Fatalf("expected schema issues, got %#v", issues)
	}
	assertValidationCode(t, issues, "type")
	assertValidationCode(t, issues, "min_items")

	_, issues = validator.Validate(validationTestSchema, "before {\"summary\":\"ok\",\"steps\":[\"one\"]}")
	assertValidationCode(t, issues, "invalid_json")

	_, issues = validator.Validate(validationTestSchema, `{"summary":"ok","steps":["one"],"extra":true}`)
	assertValidationCode(t, issues, "additional_property")
}

func TestValidationIssuesDoNotContainResponseValues(t *testing.T) {
	validator := JSONSchemaValidator{}
	secret := "must-never-appear-in-errors"
	_, issues := validator.Validate(validationTestSchema, `{"summary":1,"steps":["`+secret+`"],"extra":"`+secret+`"}`)
	encoded, err := json.Marshal(issues)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("validation issues leaked response content: %s", encoded)
	}
}

func assertValidationCode(t *testing.T, issues []ValidationIssue, code string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("expected validation code %q in %#v", code, issues)
}
