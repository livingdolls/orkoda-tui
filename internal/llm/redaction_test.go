package llm

import (
	"strings"
	"sync"
	"testing"
)

func TestStandardRedactorRemovesHighConfidenceSecrets(t *testing.T) {
	redactor := NewStandardRedactor()
	secretToken := "sk-abcdefghijklmnopqrstuvwxyz123456"
	privateKey := "-----BEGIN PRIVATE KEY-----\nvery-secret-key-material\n-----END PRIVATE KEY-----"
	request := Request{Messages: []Message{{
		Role: RoleUser,
		Content: strings.Join([]string{
			"Authorization: Bearer " + secretToken,
			"DATABASE_PASSWORD=database-secret",
			"https://user:password123@example.com/path",
			privateKey,
			"uuid=550e8400-e29b-41d4-a716-446655440000",
			"sha=0123456789abcdef0123456789abcdef01234567",
		}, "\n"),
	}}}

	redacted, report, err := redactor.Redact(request, RedactionModeStrict)
	if err != nil {
		t.Fatal(err)
	}
	content := redacted.Messages[0].Content
	for _, secret := range []string{secretToken, "database-secret", "password123", privateKey} {
		if strings.Contains(content, secret) {
			t.Fatalf("secret was not redacted: %q in %s", secret, content)
		}
	}
	for _, publicValue := range []string{
		"550e8400-e29b-41d4-a716-446655440000",
		"0123456789abcdef0123456789abcdef01234567",
	} {
		if !strings.Contains(content, publicValue) {
			t.Fatalf("public identifier was redacted: %s", publicValue)
		}
	}
	if report.Count < 4 || report.Types["PRIVATE_KEY"] != 1 {
		t.Fatalf("unexpected redaction report: %#v", report)
	}
	if strings.Contains(request.Messages[0].Content, "[REDACTED:") {
		t.Fatal("redactor mutated the original request")
	}
}

func TestStandardRedactorReportModeDoesNotModifyPrompt(t *testing.T) {
	redactor := NewStandardRedactor()
	request := Request{Messages: []Message{{Role: RoleUser, Content: "API_TOKEN=github_pat_abcdefghijklmnopqrstuvwxyz123456"}}}
	result, report, err := redactor.Redact(request, RedactionModeReport)
	if err != nil {
		t.Fatal(err)
	}
	if result.Messages[0].Content != request.Messages[0].Content {
		t.Fatal("report mode modified the request")
	}
	if report.Count == 0 {
		t.Fatal("report mode did not detect the secret")
	}
}

func TestStandardRedactorIsConcurrentSafe(t *testing.T) {
	redactor := NewStandardRedactor()
	request := Request{Messages: []Message{{Role: RoleUser, Content: "Authorization: Bearer sk-abcdefghijklmnopqrstuvwxyz123456"}}}
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, report, err := redactor.Redact(request, RedactionModeStrict)
			if err != nil || report.Count != 1 || strings.Contains(result.Messages[0].Content, "abcdefghijklmnopqrstuvwxyz") {
				t.Errorf("unexpected concurrent redaction result: report=%#v err=%v", report, err)
			}
		}()
	}
	wait.Wait()
}
