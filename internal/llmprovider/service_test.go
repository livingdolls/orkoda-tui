package llmprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/livingdolls/orkoda-tui/internal/database"
	"github.com/livingdolls/orkoda-tui/internal/llm"
	"github.com/livingdolls/orkoda-tui/internal/planningagent"
)

type memoryCredentials struct {
	mu     sync.Mutex
	values map[string]string
}

func (m *memoryCredentials) Get(_ context.Context, account string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value := m.values[account]
	if value == "" {
		return "", errors.New("missing credential")
	}
	return value, nil
}

func (m *memoryCredentials) Set(_ context.Context, account, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[account] = value
	return nil
}

func (m *memoryCredentials) Delete(_ context.Context, account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, account)
	return nil
}

func TestServiceSavesTestsAndDeletesRuntimeProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/chat/completions" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"id":    "provider-test",
			"model": "model-a",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "OK"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 4, "completion_tokens": 1, "total_tokens": 5},
		})
	}))
	defer server.Close()

	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir()+"/orkoda.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	repository, err := NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := llm.NewRegistry(planningagent.NewLocalFakeProvider())
	if err != nil {
		t.Fatal(err)
	}
	secrets := &memoryCredentials{values: map[string]string{}}
	service, err := NewService(repository, registry, secrets, "local-fake", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Load(ctx); err != nil {
		t.Fatal(err)
	}

	info, err := service.Save(ctx, "custom", SaveInput{
		BaseURL:      server.URL,
		DefaultModel: "model-a",
		APIKey:       "secret",
		JSONMode:     "json_object",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !info.Configured || !info.CredentialStored || !info.Deletable || info.Source != "tui" {
		t.Fatalf("unexpected provider info %#v", info)
	}
	if _, err := registry.Provider("custom"); err != nil {
		t.Fatal(err)
	}
	result, err := service.Test(ctx, "custom")
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != "custom" || result.Model != "model-a" || result.ResponsePreview != "OK" {
		t.Fatalf("unexpected test result %#v", result)
	}
	if _, err := service.Save(ctx, "unsafe", SaveInput{
		BaseURL: server.URL, DefaultModel: "model-a", APIKey: "provider-value",
		Headers: map[string]string{"Authorization": "Bearer should-not-enter-sqlite"},
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected sensitive header rejection, got %v", err)
	}
	if err := service.Delete(ctx, "custom"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Provider("custom"); err == nil {
		t.Fatal("deleted provider remained registered")
	}
}
