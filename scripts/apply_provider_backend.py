from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]


def write(path: str, content: str) -> None:
    target = ROOT / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(content, encoding="utf-8")


def replace(path: str, old: str, new: str, count: int = 1) -> None:
    target = ROOT / path
    content = target.read_text(encoding="utf-8")
    if old not in content:
        raise RuntimeError(f"expected snippet not found in {path}: {old[:160]!r}")
    target.write_text(content.replace(old, new, count), encoding="utf-8")


def regex_replace(path: str, pattern: str, replacement: str, count: int = 1) -> None:
    target = ROOT / path
    content = target.read_text(encoding="utf-8")
    updated, matched = re.subn(pattern, replacement, content, count=count, flags=re.S)
    if matched != count:
        raise RuntimeError(f"expected {count} regex match(es) in {path}, got {matched}: {pattern[:160]!r}")
    target.write_text(updated, encoding="utf-8")


write("internal/credentials/file.go", r'''package credentials

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "sync"
)

type CredentialStore interface {
    Get(context.Context, string) (string, error)
    Set(context.Context, string, string) error
    Delete(context.Context, string) error
}

type FileStore struct {
    path string
    mu   sync.Mutex
}

func NewFileStore(path string) (*FileStore, error) {
    path = strings.TrimSpace(path)
    if path == "" {
        return nil, fmt.Errorf("credential file path is required")
    }
    return &FileStore{path: filepath.Clean(path)}, nil
}

func (s *FileStore) Get(_ context.Context, account string) (string, error) {
    if s == nil {
        return "", ErrUnavailable
    }
    account = strings.TrimSpace(account)
    if account == "" {
        return "", fmt.Errorf("credential account is required")
    }
    s.mu.Lock()
    defer s.mu.Unlock()
    values, err := s.read()
    if err != nil {
        return "", err
    }
    value := strings.TrimSpace(values[account])
    if value == "" {
        return "", ErrNotFound
    }
    return value, nil
}

func (s *FileStore) Set(_ context.Context, account, value string) error {
    if s == nil {
        return ErrUnavailable
    }
    account = strings.TrimSpace(account)
    value = strings.TrimSpace(value)
    if account == "" || value == "" || strings.ContainsAny(value, "\r\n") {
        return fmt.Errorf("credential account and a single-line value are required")
    }
    s.mu.Lock()
    defer s.mu.Unlock()
    values, err := s.read()
    if err != nil {
        return err
    }
    values[account] = value
    return s.write(values)
}

func (s *FileStore) Delete(_ context.Context, account string) error {
    if s == nil {
        return ErrUnavailable
    }
    account = strings.TrimSpace(account)
    if account == "" {
        return fmt.Errorf("credential account is required")
    }
    s.mu.Lock()
    defer s.mu.Unlock()
    values, err := s.read()
    if err != nil {
        return err
    }
    delete(values, account)
    return s.write(values)
}

func (s *FileStore) read() (map[string]string, error) {
    content, err := os.ReadFile(s.path)
    if errors.Is(err, os.ErrNotExist) {
        return map[string]string{}, nil
    }
    if err != nil {
        return nil, fmt.Errorf("read protected credential file: %w", err)
    }
    values := map[string]string{}
    if len(content) > 0 {
        if err := json.Unmarshal(content, &values); err != nil {
            return nil, fmt.Errorf("decode protected credential file: %w", err)
        }
    }
    return values, nil
}

func (s *FileStore) write(values map[string]string) error {
    if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
        return fmt.Errorf("create credential directory: %w", err)
    }
    content, err := json.Marshal(values)
    if err != nil {
        return fmt.Errorf("encode protected credential file: %w", err)
    }
    temp, err := os.CreateTemp(filepath.Dir(s.path), ".credentials-*")
    if err != nil {
        return fmt.Errorf("create temporary credential file: %w", err)
    }
    tempPath := temp.Name()
    defer os.Remove(tempPath)
    if err := temp.Chmod(0o600); err != nil {
        temp.Close()
        return fmt.Errorf("protect temporary credential file: %w", err)
    }
    if _, err := temp.Write(content); err != nil {
        temp.Close()
        return fmt.Errorf("write temporary credential file: %w", err)
    }
    if err := temp.Sync(); err != nil {
        temp.Close()
        return fmt.Errorf("sync temporary credential file: %w", err)
    }
    if err := temp.Close(); err != nil {
        return fmt.Errorf("close temporary credential file: %w", err)
    }
    if err := os.Rename(tempPath, s.path); err != nil {
        return fmt.Errorf("replace protected credential file: %w", err)
    }
    if err := os.Chmod(s.path, 0o600); err != nil {
        return fmt.Errorf("protect credential file: %w", err)
    }
    return nil
}

type AutoStore struct {
    keychain *Store
    fallback *FileStore
}

func NewAutoStore(service, fallbackPath string) (*AutoStore, error) {
    keychain, err := NewOSStore(service)
    if err != nil {
        return nil, err
    }
    fallback, err := NewFileStore(fallbackPath)
    if err != nil {
        return nil, err
    }
    return &AutoStore{keychain: keychain, fallback: fallback}, nil
}

func (s *AutoStore) Get(ctx context.Context, account string) (string, error) {
    if s == nil || s.keychain == nil || s.fallback == nil {
        return "", ErrUnavailable
    }
    value, err := s.keychain.Get(ctx, account)
    if err == nil {
        return value, nil
    }
    if !errors.Is(err, ErrUnavailable) && !errors.Is(err, ErrNotFound) {
        return "", err
    }
    return s.fallback.Get(ctx, account)
}

func (s *AutoStore) Set(ctx context.Context, account, value string) error {
    if s == nil || s.keychain == nil || s.fallback == nil {
        return ErrUnavailable
    }
    if err := s.keychain.Set(ctx, account, value); err == nil {
        _ = s.fallback.Delete(ctx, account)
        return nil
    } else if !errors.Is(err, ErrUnavailable) {
        return err
    }
    return s.fallback.Set(ctx, account, value)
}

func (s *AutoStore) Delete(ctx context.Context, account string) error {
    if s == nil || s.keychain == nil || s.fallback == nil {
        return ErrUnavailable
    }
    keychainErr := s.keychain.Delete(ctx, account)
    fileErr := s.fallback.Delete(ctx, account)
    if keychainErr != nil && !errors.Is(keychainErr, ErrUnavailable) && !errors.Is(keychainErr, ErrNotFound) {
        return keychainErr
    }
    if fileErr != nil && !errors.Is(fileErr, ErrNotFound) {
        return fileErr
    }
    return nil
}

var _ CredentialStore = (*Store)(nil)
var _ CredentialStore = (*FileStore)(nil)
var _ CredentialStore = (*AutoStore)(nil)
''')

write("internal/credentials/file_test.go", r'''package credentials

import (
    "context"
    "errors"
    "os"
    "path/filepath"
    "testing"
)

func TestFileStoreRoundTripAndPermissions(t *testing.T) {
    path := filepath.Join(t.TempDir(), "credentials.json")
    store, err := NewFileStore(path)
    if err != nil {
        t.Fatal(err)
    }
    ctx := context.Background()
    if err := store.Set(ctx, "llm-provider:deepseek", "secret-value"); err != nil {
        t.Fatal(err)
    }
    value, err := store.Get(ctx, "llm-provider:deepseek")
    if err != nil {
        t.Fatal(err)
    }
    if value != "secret-value" {
        t.Fatalf("unexpected credential %q", value)
    }
    info, err := os.Stat(path)
    if err != nil {
        t.Fatal(err)
    }
    if info.Mode().Perm() != 0o600 {
        t.Fatalf("credential file permissions are %o", info.Mode().Perm())
    }
    if err := store.Delete(ctx, "llm-provider:deepseek"); err != nil {
        t.Fatal(err)
    }
    if _, err := store.Get(ctx, "llm-provider:deepseek"); !errors.Is(err, ErrNotFound) {
        t.Fatalf("expected not found, got %v", err)
    }
}
''')

write("internal/llmprovider/repository.go", r'''package llmprovider

import (
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "fmt"
    "strings"
    "time"
)

var ErrNotFound = errors.New("LLM provider configuration not found")

type Record struct {
    Name         string
    BaseURL      string
    DefaultModel string
    JSONMode     string
    Timeout      time.Duration
    Headers      map[string]string
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

type Repository struct {
    db *sql.DB
}

func NewRepository(db *sql.DB) (*Repository, error) {
    if db == nil {
        return nil, fmt.Errorf("LLM provider database is required")
    }
    return &Repository{db: db}, nil
}

func (r *Repository) List(ctx context.Context) ([]Record, error) {
    if r == nil || r.db == nil {
        return nil, fmt.Errorf("LLM provider repository is unavailable")
    }
    rows, err := r.db.QueryContext(ctx, `
        SELECT name, base_url, default_model, json_mode, timeout_ms, headers_json, created_at, updated_at
        FROM llm_provider_configs
        ORDER BY name
    `)
    if err != nil {
        return nil, fmt.Errorf("list LLM provider configurations: %w", err)
    }
    defer rows.Close()
    items := []Record{}
    for rows.Next() {
        item, err := scanRecord(rows)
        if err != nil {
            return nil, err
        }
        items = append(items, item)
    }
    if err := rows.Err(); err != nil {
        return nil, fmt.Errorf("read LLM provider configurations: %w", err)
    }
    return items, nil
}

func (r *Repository) Get(ctx context.Context, name string) (Record, error) {
    if r == nil || r.db == nil {
        return Record{}, fmt.Errorf("LLM provider repository is unavailable")
    }
    row := r.db.QueryRowContext(ctx, `
        SELECT name, base_url, default_model, json_mode, timeout_ms, headers_json, created_at, updated_at
        FROM llm_provider_configs
        WHERE name = ?
    `, strings.ToLower(strings.TrimSpace(name)))
    item, err := scanRecord(row)
    if errors.Is(err, sql.ErrNoRows) {
        return Record{}, ErrNotFound
    }
    return item, err
}

func (r *Repository) Upsert(ctx context.Context, item Record) (Record, error) {
    if r == nil || r.db == nil {
        return Record{}, fmt.Errorf("LLM provider repository is unavailable")
    }
    headers, err := json.Marshal(item.Headers)
    if err != nil {
        return Record{}, fmt.Errorf("encode LLM provider headers: %w", err)
    }
    now := time.Now().UTC().UnixMilli()
    if _, err := r.db.ExecContext(ctx, `
        INSERT INTO llm_provider_configs(
            name, base_url, default_model, json_mode, timeout_ms, headers_json, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(name) DO UPDATE SET
            base_url = excluded.base_url,
            default_model = excluded.default_model,
            json_mode = excluded.json_mode,
            timeout_ms = excluded.timeout_ms,
            headers_json = excluded.headers_json,
            updated_at = excluded.updated_at
    `, item.Name, item.BaseURL, item.DefaultModel, item.JSONMode, item.Timeout.Milliseconds(), string(headers), now, now); err != nil {
        return Record{}, fmt.Errorf("save LLM provider configuration: %w", err)
    }
    return r.Get(ctx, item.Name)
}

func (r *Repository) Delete(ctx context.Context, name string) error {
    if r == nil || r.db == nil {
        return fmt.Errorf("LLM provider repository is unavailable")
    }
    result, err := r.db.ExecContext(ctx, `DELETE FROM llm_provider_configs WHERE name = ?`, strings.ToLower(strings.TrimSpace(name)))
    if err != nil {
        return fmt.Errorf("delete LLM provider configuration: %w", err)
    }
    affected, err := result.RowsAffected()
    if err != nil {
        return fmt.Errorf("read deleted LLM provider count: %w", err)
    }
    if affected == 0 {
        return ErrNotFound
    }
    return nil
}

type rowScanner interface {
    Scan(...any) error
}

func scanRecord(row rowScanner) (Record, error) {
    var item Record
    var timeoutMS, createdAt, updatedAt int64
    var headersJSON string
    if err := row.Scan(
        &item.Name, &item.BaseURL, &item.DefaultModel, &item.JSONMode,
        &timeoutMS, &headersJSON, &createdAt, &updatedAt,
    ); err != nil {
        return Record{}, err
    }
    if err := json.Unmarshal([]byte(headersJSON), &item.Headers); err != nil {
        return Record{}, fmt.Errorf("decode LLM provider headers: %w", err)
    }
    item.Timeout = time.Duration(timeoutMS) * time.Millisecond
    item.CreatedAt = time.UnixMilli(createdAt).UTC()
    item.UpdatedAt = time.UnixMilli(updatedAt).UTC()
    return item, nil
}
''')

write("internal/llmprovider/service.go", r'''package llmprovider

import (
    "context"
    "errors"
    "fmt"
    "net"
    "net/url"
    "sort"
    "strings"
    "sync"
    "time"

    "github.com/livingdolls/orkoda-tui/internal/credentials"
    "github.com/livingdolls/orkoda-tui/internal/llm"
    "github.com/livingdolls/orkoda-tui/internal/llm/openaicompat"
)

var (
    ErrInvalid  = errors.New("invalid LLM provider configuration")
    ErrReadOnly = errors.New("LLM provider is read-only")
)

const credentialPrefix = "llm-provider:"

type Bootstrap struct {
    Provider     llm.Provider
    BaseURL      string
    DefaultModel string
    JSONMode     string
    Timeout      time.Duration
}

type SaveInput struct {
    BaseURL      string            `json:"base_url"`
    DefaultModel string            `json:"default_model"`
    APIKey       string            `json:"api_key,omitempty"`
    JSONMode     string            `json:"json_mode,omitempty"`
    TimeoutMS    int64             `json:"timeout_ms,omitempty"`
    Headers      map[string]string `json:"headers,omitempty"`
}

type TestResult struct {
    Provider        string `json:"provider"`
    Model           string `json:"model"`
    LatencyMS       int64  `json:"latency_ms"`
    ResponsePreview string `json:"response_preview"`
}

type Service struct {
    repository      *Repository
    registry        *llm.Registry
    credentials     credentials.CredentialStore
    defaultProvider string

    mu                 sync.RWMutex
    managed            map[string]Record
    managedCredentials map[string]bool
    bootstrap          map[string]Bootstrap
}

func NewService(
    repository *Repository,
    registry *llm.Registry,
    credentialStore credentials.CredentialStore,
    defaultProvider string,
    bootstraps []Bootstrap,
) (*Service, error) {
    if repository == nil || registry == nil || credentialStore == nil {
        return nil, fmt.Errorf("LLM provider repository, registry, and credential store are required")
    }
    service := &Service{
        repository: repository,
        registry: registry,
        credentials: credentialStore,
        defaultProvider: strings.ToLower(strings.TrimSpace(defaultProvider)),
        managed: map[string]Record{},
        managedCredentials: map[string]bool{},
        bootstrap: map[string]Bootstrap{},
    }
    for _, bootstrap := range bootstraps {
        if bootstrap.Provider == nil {
            return nil, fmt.Errorf("bootstrap LLM provider is required")
        }
        name := normalizeName(bootstrap.Provider.Name())
        if name == "" {
            return nil, fmt.Errorf("bootstrap LLM provider name is required")
        }
        bootstrap.DefaultModel = strings.TrimSpace(bootstrap.DefaultModel)
        bootstrap.BaseURL = strings.TrimRight(strings.TrimSpace(bootstrap.BaseURL), "/")
        bootstrap.JSONMode = strings.ToLower(strings.TrimSpace(bootstrap.JSONMode))
        service.bootstrap[name] = bootstrap
    }
    return service, nil
}

func (s *Service) Load(ctx context.Context) error {
    if s == nil {
        return fmt.Errorf("LLM provider service is unavailable")
    }
    for _, bootstrap := range s.bootstrap {
        if err := s.registry.Upsert(bootstrap.Provider); err != nil {
            return err
        }
    }
    items, err := s.repository.List(ctx)
    if err != nil {
        return err
    }
    managed := make(map[string]Record, len(items))
    credentialState := make(map[string]bool, len(items))
    for _, item := range items {
        name := normalizeName(item.Name)
        managed[name] = item
        apiKey, err := s.credentials.Get(ctx, credentialAccount(name))
        if errors.Is(err, credentials.ErrNotFound) || errors.Is(err, credentials.ErrUnavailable) {
            credentialState[name] = false
            continue
        }
        if err != nil {
            return fmt.Errorf("load credential for LLM provider %s: %w", name, err)
        }
        provider, err := buildProvider(item, apiKey)
        if err != nil {
            return fmt.Errorf("restore LLM provider %s: %w", name, err)
        }
        if err := s.registry.Upsert(provider); err != nil {
            return err
        }
        credentialState[name] = true
    }
    s.mu.Lock()
    s.managed = managed
    s.managedCredentials = credentialState
    s.mu.Unlock()
    return nil
}

func (s *Service) List() []llm.ProviderInfo {
    if s == nil || s.registry == nil {
        return []llm.ProviderInfo{}
    }
    infos := llm.NewCatalog(s.registry, s.defaultProvider).List()
    byName := make(map[string]int, len(infos))
    for index := range infos {
        name := normalizeName(infos[index].Name)
        infos[index].Name = name
        infos[index].Source = "runtime"
        infos[index].Editable = name != "local-fake"
        byName[name] = index
    }
    for name, bootstrap := range s.bootstrap {
        index, exists := byName[name]
        if !exists {
            continue
        }
        infos[index].BaseURL = bootstrap.BaseURL
        infos[index].JSONMode = bootstrap.JSONMode
        infos[index].TimeoutMS = bootstrap.Timeout.Milliseconds()
        infos[index].CredentialStored = true
        infos[index].Source = "environment"
        infos[index].Editable = true
    }
    s.mu.RLock()
    for name, item := range s.managed {
        credentialStored := s.managedCredentials[name]
        index, exists := byName[name]
        if !exists {
            infos = append(infos, llm.ProviderInfo{Name: name})
            index = len(infos) - 1
            byName[name] = index
        }
        infos[index].DefaultModel = item.DefaultModel
        infos[index].BaseURL = item.BaseURL
        infos[index].JSONMode = item.JSONMode
        infos[index].TimeoutMS = item.Timeout.Milliseconds()
        infos[index].CredentialStored = credentialStored
        infos[index].Configured = credentialStored
        infos[index].StructuredOutput = item.JSONMode != "prompt_only"
        infos[index].Source = "tui"
        infos[index].Editable = true
        infos[index].Deletable = true
        infos[index].Default = strings.EqualFold(name, s.defaultProvider)
    }
    s.mu.RUnlock()
    for index := range infos {
        if infos[index].Source == "runtime" && infos[index].Name == "local-fake" {
            infos[index].Source = "built-in"
            infos[index].CredentialStored = true
            infos[index].Editable = false
        }
    }
    sort.Slice(infos, func(left, right int) bool {
        if infos[left].Default != infos[right].Default {
            return infos[left].Default
        }
        return infos[left].Name < infos[right].Name
    })
    return infos
}

func (s *Service) Save(ctx context.Context, name string, input SaveInput) (llm.ProviderInfo, error) {
    if s == nil {
        return llm.ProviderInfo{}, fmt.Errorf("LLM provider service is unavailable")
    }
    name = normalizeName(name)
    if name == "" || name == "local-fake" {
        return llm.ProviderInfo{}, fmt.Errorf("%w: provider name must identify an external provider", ErrInvalid)
    }
    s.mu.RLock()
    existing, hasExisting := s.managed[name]
    s.mu.RUnlock()
    record, err := normalizeRecord(name, input, existing, hasExisting)
    if err != nil {
        return llm.ProviderInfo{}, err
    }
    apiKey := strings.TrimSpace(input.APIKey)
    if apiKey == "" && hasExisting {
        apiKey, err = s.credentials.Get(ctx, credentialAccount(name))
        if err != nil {
            return llm.ProviderInfo{}, fmt.Errorf("existing API key is unavailable; enter it again: %w", err)
        }
    }
    if apiKey == "" {
        return llm.ProviderInfo{}, fmt.Errorf("%w: API key is required when adding a provider", ErrInvalid)
    }
    provider, err := buildProvider(record, apiKey)
    if err != nil {
        return llm.ProviderInfo{}, err
    }
    if strings.TrimSpace(input.APIKey) != "" {
        if err := s.credentials.Set(ctx, credentialAccount(name), apiKey); err != nil {
            return llm.ProviderInfo{}, fmt.Errorf("store LLM provider credential: %w", err)
        }
    }
    saved, err := s.repository.Upsert(ctx, record)
    if err != nil {
        return llm.ProviderInfo{}, err
    }
    if err := s.registry.Upsert(provider); err != nil {
        return llm.ProviderInfo{}, err
    }
    s.mu.Lock()
    s.managed[name] = saved
    s.managedCredentials[name] = true
    s.mu.Unlock()
    return s.info(name), nil
}

func (s *Service) Delete(ctx context.Context, name string) error {
    if s == nil {
        return fmt.Errorf("LLM provider service is unavailable")
    }
    name = normalizeName(name)
    s.mu.RLock()
    _, managed := s.managed[name]
    bootstrap, bootstrapped := s.bootstrap[name]
    s.mu.RUnlock()
    if !managed {
        if bootstrapped || name == "local-fake" {
            return fmt.Errorf("%w: provider %s is managed outside the TUI", ErrReadOnly, name)
        }
        return ErrNotFound
    }
    if err := s.repository.Delete(ctx, name); err != nil {
        return err
    }
    if err := s.credentials.Delete(ctx, credentialAccount(name)); err != nil &&
        !errors.Is(err, credentials.ErrNotFound) && !errors.Is(err, credentials.ErrUnavailable) {
        return err
    }
    if bootstrapped {
        if err := s.registry.Upsert(bootstrap.Provider); err != nil {
            return err
        }
    } else {
        s.registry.Remove(name)
    }
    s.mu.Lock()
    delete(s.managed, name)
    delete(s.managedCredentials, name)
    s.mu.Unlock()
    return nil
}

func (s *Service) Test(ctx context.Context, name string) (TestResult, error) {
    if s == nil {
        return TestResult{}, fmt.Errorf("LLM provider service is unavailable")
    }
    name = normalizeName(name)
    provider, err := s.registry.Provider(name)
    if err != nil {
        return TestResult{}, ErrNotFound
    }
    model := ""
    s.mu.RLock()
    if item, exists := s.managed[name]; exists {
        model = item.DefaultModel
    } else if item, exists := s.bootstrap[name]; exists {
        model = item.DefaultModel
    }
    s.mu.RUnlock()
    if model == "" {
        if describer, ok := provider.(llm.ProviderDescriber); ok {
            model = describer.Info().DefaultModel
        }
    }
    if model == "" {
        return TestResult{}, fmt.Errorf("%w: provider model is not configured", ErrInvalid)
    }
    testCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()
    started := time.Now()
    response, err := provider.Complete(testCtx, llm.Request{
        Model: model,
        Messages: []llm.Message{
            {Role: llm.RoleSystem, Content: "You are a connectivity test. Reply with exactly OK."},
            {Role: llm.RoleUser, Content: "Reply OK."},
        },
        MaxOutputTokens: 16,
        Temperature: 0,
    })
    if err != nil {
        return TestResult{}, fmt.Errorf("provider test failed: %w", err)
    }
    preview := strings.TrimSpace(response.Content)
    if len(preview) > 120 {
        preview = preview[:120]
    }
    return TestResult{
        Provider: name,
        Model: response.Model,
        LatencyMS: time.Since(started).Milliseconds(),
        ResponsePreview: preview,
    }, nil
}

func (s *Service) info(name string) llm.ProviderInfo {
    for _, info := range s.List() {
        if info.Name == name {
            return info
        }
    }
    return llm.ProviderInfo{Name: name}
}

func normalizeRecord(name string, input SaveInput, existing Record, hasExisting bool) (Record, error) {
    baseURL, err := validateBaseURL(input.BaseURL)
    if err != nil {
        return Record{}, err
    }
    model := strings.TrimSpace(input.DefaultModel)
    if model == "" {
        return Record{}, fmt.Errorf("%w: default model is required", ErrInvalid)
    }
    jsonMode := strings.ToLower(strings.TrimSpace(input.JSONMode))
    if jsonMode == "" {
        jsonMode = "json_schema"
    }
    switch jsonMode {
    case "json_schema", "json_object", "prompt_only":
    default:
        return Record{}, fmt.Errorf("%w: JSON mode must be json_schema, json_object, or prompt_only", ErrInvalid)
    }
    timeout := time.Duration(input.TimeoutMS) * time.Millisecond
    if timeout <= 0 {
        timeout = 60 * time.Second
    }
    if timeout < time.Second || timeout > 10*time.Minute {
        return Record{}, fmt.Errorf("%w: timeout must be between 1 second and 10 minutes", ErrInvalid)
    }
    headers := input.Headers
    if headers == nil && hasExisting {
        headers = existing.Headers
    }
    if headers == nil {
        headers = map[string]string{}
    }
    cleanHeaders := make(map[string]string, len(headers))
    for key, value := range headers {
        key = strings.TrimSpace(key)
        value = strings.TrimSpace(value)
        if key != "" && value != "" {
            cleanHeaders[key] = value
        }
    }
    return Record{
        Name: name,
        BaseURL: baseURL,
        DefaultModel: model,
        JSONMode: jsonMode,
        Timeout: timeout,
        Headers: cleanHeaders,
    }, nil
}

func validateBaseURL(value string) (string, error) {
    value = strings.TrimRight(strings.TrimSpace(value), "/")
    parsed, err := url.Parse(value)
    if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil ||
        parsed.RawQuery != "" || parsed.Fragment != "" {
        return "", fmt.Errorf("%w: base URL must be an absolute HTTP(S) URL without credentials, query, or fragment", ErrInvalid)
    }
    if parsed.Scheme != "https" {
        host := parsed.Hostname()
        ip := net.ParseIP(host)
        if parsed.Scheme != "http" || (host != "localhost" && (ip == nil || !ip.IsLoopback())) {
            return "", fmt.Errorf("%w: non-local provider URLs must use HTTPS", ErrInvalid)
        }
    }
    return value, nil
}

func buildProvider(item Record, apiKey string) (llm.Provider, error) {
    provider, err := openaicompat.New(openaicompat.Config{
        Name: item.Name,
        BaseURL: item.BaseURL,
        APIKey: apiKey,
        DefaultModel: item.DefaultModel,
        Headers: item.Headers,
        Timeout: item.Timeout,
        JSONMode: openaicompat.JSONMode(item.JSONMode),
    })
    if err != nil {
        return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
    }
    return provider, nil
}

func normalizeName(name string) string {
    name = strings.ToLower(strings.TrimSpace(name))
    if len(name) == 0 || len(name) > 64 {
        return ""
    }
    for index, character := range name {
        if (character < 'a' || character > 'z') &&
            (character < '0' || character > '9') &&
            character != '-' && character != '_' && character != '.' {
            return ""
        }
        if index == 0 && (character == '-' || character == '_' || character == '.') {
            return ""
        }
    }
    return name
}

func credentialAccount(name string) string {
    return credentialPrefix + normalizeName(name)
}
''')

write("internal/llmprovider/service_test.go", r'''package llmprovider

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
            "id": "provider-test",
            "model": "model-a",
            "choices": []map[string]any{{
                "index": 0,
                "message": map[string]any{"role": "assistant", "content": "OK"},
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
        BaseURL: server.URL,
        DefaultModel: "model-a",
        APIKey: "secret",
        JSONMode: "json_object",
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
    if err := service.Delete(ctx, "custom"); err != nil {
        t.Fatal(err)
    }
    if _, err := registry.Provider("custom"); err == nil {
        t.Fatal("deleted provider remained registered")
    }
}
''')

replace("internal/database/migrate.go", "const latestSchemaVersion = 3", "const latestSchemaVersion = 4")
replace(
    "internal/database/migrate.go",
    '''\tif err := tx.Commit(); err != nil {
\t\treturn fmt.Errorf("commit migration: %w", err)
\t}''',
    '''\tif version < 4 {
\t\tif _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS llm_provider_configs (
\t\t\tname TEXT PRIMARY KEY,
\t\t\tbase_url TEXT NOT NULL,
\t\t\tdefault_model TEXT NOT NULL,
\t\t\tjson_mode TEXT NOT NULL CHECK (json_mode IN ('json_schema','json_object','prompt_only')),
\t\t\ttimeout_ms INTEGER NOT NULL CHECK (timeout_ms >= 1000 AND timeout_ms <= 600000),
\t\t\theaders_json TEXT NOT NULL DEFAULT '{}',
\t\t\tcreated_at INTEGER NOT NULL,
\t\t\tupdated_at INTEGER NOT NULL
\t\t)`); err != nil {
\t\t\treturn fmt.Errorf("create LLM provider configuration table: %w", err)
\t\t}
\t\tif _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, name, applied_at) VALUES (4, 'tui-managed-llm-providers', strftime('%s','now') * 1000)`); err != nil {
\t\t\treturn fmt.Errorf("record LLM provider migration: %w", err)
\t\t}
\t}

\tif err := tx.Commit(); err != nil {
\t\treturn fmt.Errorf("commit migration: %w", err)
\t}''',
)

replace(
    "internal/llm/types.go",
    '''type ProviderInfo struct {
\tName             string `json:"name"`
\tDefaultModel     string `json:"default_model"`
\tConfigured       bool   `json:"configured"`
\tStructuredOutput bool   `json:"structured_output"`
\tDefault          bool   `json:"default"`
}''',
    '''type ProviderInfo struct {
\tName             string `json:"name"`
\tDefaultModel     string `json:"default_model"`
\tConfigured       bool   `json:"configured"`
\tStructuredOutput bool   `json:"structured_output"`
\tDefault          bool   `json:"default"`
\tBaseURL          string `json:"base_url,omitempty"`
\tJSONMode         string `json:"json_mode,omitempty"`
\tTimeoutMS        int64  `json:"timeout_ms,omitempty"`
\tCredentialStored bool   `json:"credential_stored"`
\tSource           string `json:"source,omitempty"`
\tEditable         bool   `json:"editable"`
\tDeletable        bool   `json:"deletable"`
}''',
)

replace(
    "internal/llm/registry.go",
    '''func (r *Registry) Provider(name string) (Provider, error) {''',
    '''func (r *Registry) Upsert(provider Provider) error {
\tif r == nil {
\t\treturn fmt.Errorf("LLM registry is required")
\t}
\tif provider == nil {
\t\treturn fmt.Errorf("LLM provider is required")
\t}
\tname := normalizeProviderName(provider.Name())
\tif name == "" {
\t\treturn fmt.Errorf("LLM provider name is required")
\t}
\tr.mu.Lock()
\tr.providers[name] = provider
\tr.mu.Unlock()
\treturn nil
}

func (r *Registry) Remove(name string) bool {
\tif r == nil {
\t\treturn false
\t}
\tname = normalizeProviderName(name)
\tif name == "" {
\t\treturn false
\t}
\tr.mu.Lock()
\t_, exists := r.providers[name]
\tdelete(r.providers, name)
\tr.mu.Unlock()
\treturn exists
}

func (r *Registry) Provider(name string) (Provider, error) {''',
)

write("internal/httpapi/llm.go", r'''package httpapi

import (
    "context"
    "errors"
    "net/http"
    "strings"

    "github.com/gin-gonic/gin"
    "github.com/livingdolls/orkoda-tui/internal/credentials"
    "github.com/livingdolls/orkoda-tui/internal/llm"
    "github.com/livingdolls/orkoda-tui/internal/llmprovider"
)

type LLMProviderCatalog interface {
    List() []llm.ProviderInfo
}

type LLMProviderAdmin interface {
    Save(context.Context, string, llmprovider.SaveInput) (llm.ProviderInfo, error)
    Delete(context.Context, string) error
    Test(context.Context, string) (llmprovider.TestResult, error)
}

type LLMPolicyReader interface {
    Info() llm.PolicyInfo
}

func registerLLMRoutes(
    api *gin.RouterGroup,
    catalog LLMProviderCatalog,
    admin LLMProviderAdmin,
    policyReader LLMPolicyReader,
) {
    api.GET("/llm/providers", func(c *gin.Context) {
        if catalog == nil {
            writeData(c, http.StatusOK, []llm.ProviderInfo{})
            return
        }
        writeData(c, http.StatusOK, catalog.List())
    })

    api.PUT("/llm/providers/:provider", func(c *gin.Context) {
        if admin == nil {
            writeError(c, http.StatusServiceUnavailable, "LLM provider settings are unavailable")
            return
        }
        var input llmprovider.SaveInput
        if err := c.ShouldBindJSON(&input); err != nil {
            writeError(c, http.StatusBadRequest, "invalid LLM provider configuration")
            return
        }
        item, err := admin.Save(c.Request.Context(), strings.TrimSpace(c.Param("provider")), input)
        if err != nil {
            writeLLMProviderError(c, err)
            return
        }
        writeData(c, http.StatusOK, item)
    })

    api.DELETE("/llm/providers/:provider", func(c *gin.Context) {
        if admin == nil {
            writeError(c, http.StatusServiceUnavailable, "LLM provider settings are unavailable")
            return
        }
        if err := admin.Delete(c.Request.Context(), strings.TrimSpace(c.Param("provider"))); err != nil {
            writeLLMProviderError(c, err)
            return
        }
        c.Status(http.StatusNoContent)
    })

    api.POST("/llm/providers/:provider/test", func(c *gin.Context) {
        if admin == nil {
            writeError(c, http.StatusServiceUnavailable, "LLM provider settings are unavailable")
            return
        }
        result, err := admin.Test(c.Request.Context(), strings.TrimSpace(c.Param("provider")))
        if err != nil {
            writeLLMProviderError(c, err)
            return
        }
        writeData(c, http.StatusOK, result)
    })

    api.GET("/llm/policy", func(c *gin.Context) {
        if policyReader == nil {
            writeData(c, http.StatusOK, llm.PolicyInfo{})
            return
        }
        writeData(c, http.StatusOK, policyReader.Info())
    })
}

func writeLLMProviderError(c *gin.Context, err error) {
    switch {
    case errors.Is(err, llmprovider.ErrNotFound):
        writeError(c, http.StatusNotFound, err.Error())
    case errors.Is(err, llmprovider.ErrReadOnly):
        writeError(c, http.StatusConflict, err.Error())
    case errors.Is(err, llmprovider.ErrInvalid):
        writeError(c, http.StatusBadRequest, err.Error())
    case errors.Is(err, credentials.ErrUnavailable):
        writeError(c, http.StatusServiceUnavailable, "secure credential storage is unavailable")
    default:
        writeError(c, http.StatusUnprocessableEntity, err.Error())
    }
}
''')

replace(
    "internal/httpapi/router.go",
    '''\tLLMProviders        LLMProviderCatalog
\tLLMPolicy           LLMPolicyReader''',
    '''\tLLMProviders        LLMProviderCatalog
\tLLMProviderAdmin    LLMProviderAdmin
\tLLMPolicy           LLMPolicyReader''',
)
replace(
    "internal/httpapi/router.go",
    '''\tregisterLLMRoutes(api, services.LLMProviders, services.LLMPolicy)''',
    '''\tregisterLLMRoutes(api, services.LLMProviders, services.LLMProviderAdmin, services.LLMPolicy)''',
)

write("internal/httpapi/llm_test.go", r'''package httpapi

import (
    "context"
    "encoding/json"
    "net/http"
    "strings"
    "testing"

    "github.com/gin-gonic/gin"
    "github.com/livingdolls/orkoda-tui/internal/llm"
    "github.com/livingdolls/orkoda-tui/internal/llmprovider"
)

type providerCatalogStub struct {
    items []llm.ProviderInfo
}

func (s providerCatalogStub) List() []llm.ProviderInfo {
    return append([]llm.ProviderInfo(nil), s.items...)
}

type providerAdminStub struct {
    saved llmprovider.SaveInput
}

func (s *providerAdminStub) Save(_ context.Context, name string, input llmprovider.SaveInput) (llm.ProviderInfo, error) {
    s.saved = input
    return llm.ProviderInfo{
        Name: name, DefaultModel: input.DefaultModel, BaseURL: input.BaseURL,
        Configured: true, CredentialStored: true, Source: "tui", Editable: true, Deletable: true,
    }, nil
}
func (s *providerAdminStub) Delete(context.Context, string) error { return nil }
func (s *providerAdminStub) Test(_ context.Context, name string) (llmprovider.TestResult, error) {
    return llmprovider.TestResult{Provider: name, Model: "model-a", LatencyMS: 12, ResponsePreview: "OK"}, nil
}

type policyReaderStub struct {
    info llm.PolicyInfo
}

func (s policyReaderStub) Info() llm.PolicyInfo { return s.info }

func TestLLMProviderRoutes(t *testing.T) {
    gin.SetMode(gin.TestMode)
    router := gin.New()
    admin := &providerAdminStub{}
    registerLLMRoutes(
        router.Group("/api/v1"),
        providerCatalogStub{items: []llm.ProviderInfo{{
            Name: "openrouter", DefaultModel: "example/model", Configured: true,
            StructuredOutput: true, Default: true,
        }}},
        admin,
        policyReaderStub{info: llm.PolicyInfo{
            AttemptTimeoutMS: 45000, MaxWallClockMS: 120000, MaxAttempts: 3,
            Fallbacks: []llm.FallbackTarget{{Provider: "local-fake", Model: "local-fake-planner-v1"}},
            Budget: llm.TokenBudget{MaxTotalTokens: 60000}, RedactionMode: "strict",
            StructuredValidation: true, MaxRepairAttempts: 1, MaxStructuredResponseBytes: 1 << 20,
        }},
    )

    response := performRequest(router, http.MethodGet, "/api/v1/llm/providers", "")
    if response.Code != http.StatusOK {
        t.Fatalf("unexpected provider response: %d %s", response.Code, response.Body.String())
    }
    var providersPayload struct {
        Data []llm.ProviderInfo `json:"data"`
    }
    if err := json.Unmarshal(response.Body.Bytes(), &providersPayload); err != nil {
        t.Fatal(err)
    }
    if len(providersPayload.Data) != 1 || providersPayload.Data[0].Name != "openrouter" || !providersPayload.Data[0].Default {
        t.Fatalf("unexpected provider payload %#v", providersPayload.Data)
    }

    response = performRequest(router, http.MethodPut, "/api/v1/llm/providers/deepseek", `{
        "base_url":"https://api.deepseek.com",
        "default_model":"deepseek-v4-flash",
        "api_key":"secret",
        "json_mode":"json_object"
    }`)
    if response.Code != http.StatusOK {
        t.Fatalf("unexpected save response: %d %s", response.Code, response.Body.String())
    }
    if admin.saved.APIKey != "secret" || admin.saved.DefaultModel != "deepseek-v4-flash" {
        t.Fatalf("unexpected saved input %#v", admin.saved)
    }

    response = performRequest(router, http.MethodPost, "/api/v1/llm/providers/deepseek/test", "")
    if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"response_preview":"OK"`) {
        t.Fatalf("unexpected test response: %d %s", response.Code, response.Body.String())
    }

    response = performRequest(router, http.MethodDelete, "/api/v1/llm/providers/deepseek", "")
    if response.Code != http.StatusNoContent {
        t.Fatalf("unexpected delete response: %d %s", response.Code, response.Body.String())
    }

    response = performRequest(router, http.MethodGet, "/api/v1/llm/policy", "")
    if response.Code != http.StatusOK {
        t.Fatalf("unexpected policy response: %d %s", response.Code, response.Body.String())
    }
    var policyPayload struct {
        Data llm.PolicyInfo `json:"data"`
    }
    if err := json.Unmarshal(response.Body.Bytes(), &policyPayload); err != nil {
        t.Fatal(err)
    }
    if policyPayload.Data.MaxAttempts != 3 || policyPayload.Data.Budget.MaxTotalTokens != 60000 {
        t.Fatalf("unexpected policy payload %#v", policyPayload.Data)
    }
}
''')

replace(
    "cmd/api/main.go",
    '''\t"github.com/livingdolls/orkoda-tui/internal/llm"
\t"github.com/livingdolls/orkoda-tui/internal/llm/openaicompat"''',
    '''\t"github.com/livingdolls/orkoda-tui/internal/llm"
\t"github.com/livingdolls/orkoda-tui/internal/llm/openaicompat"
\t"github.com/livingdolls/orkoda-tui/internal/llmprovider"''',
)

regex_replace(
    "cmd/api/main.go",
    r'''\tlocalPlanningProvider := planningagent\.NewLocalFakeProvider\(\)
\tproviderRegistry, err := llm\.NewRegistry\(localPlanningProvider\)
\tif err != nil \{
\t\treturn err
\t\}
\tfor _, configured := range cfg\.LLM\.Providers \{
\t\tprovider, err := openaicompat\.New\(openaicompat\.Config\{
\t\t\tName: configured\.Name, BaseURL: configured\.BaseURL, APIKey: configured\.APIKey,
\t\t\tDefaultModel: configured\.Model, Headers: configured\.Headers,
\t\t\tTimeout: configured\.Timeout, JSONMode: openaicompat\.JSONMode\(configured\.JSONMode\),
\t\t\}\)
\t\tif err != nil \{
\t\t\treturn fmt\.Errorf\("configure LLM provider %s: %w", configured\.Name, err\)
\t\t\}
\t\tif err := providerRegistry\.Register\(provider\); err != nil \{
\t\t\treturn err
\t\t\}
\t\}
\tdefaultProvider := cfg\.LLM\.Provider
\tdefaultModel := cfg\.LLM\.Model
\tif defaultProvider == "" \|\| defaultProvider == planningagent\.LocalFakeProviderName \{
\t\tdefaultProvider = planningagent\.LocalFakeProviderName
\t\tdefaultModel = planningagent\.LocalFakeModelName
\t\}
\tproviderCatalog := llm\.NewCatalog\(providerRegistry, defaultProvider\)''',
    r'''\tcredentialStore, err := credentials.NewAutoStore("orkoda", filepath.Join(cfg.DataDir, "credentials.json"))
\tif err != nil {
\t\treturn err
\t}
\tlocalPlanningProvider := planningagent.NewLocalFakeProvider()
\tproviderRegistry, err := llm.NewRegistry(localPlanningProvider)
\tif err != nil {
\t\treturn err
\t}
\tbootstrapProviders := make([]llmprovider.Bootstrap, 0, len(cfg.LLM.Providers))
\tfor _, configured := range cfg.LLM.Providers {
\t\tprovider, err := openaicompat.New(openaicompat.Config{
\t\t\tName: configured.Name, BaseURL: configured.BaseURL, APIKey: configured.APIKey,
\t\t\tDefaultModel: configured.Model, Headers: configured.Headers,
\t\t\tTimeout: configured.Timeout, JSONMode: openaicompat.JSONMode(configured.JSONMode),
\t\t})
\t\tif err != nil {
\t\t\treturn fmt.Errorf("configure LLM provider %s: %w", configured.Name, err)
\t\t}
\t\tbootstrapProviders = append(bootstrapProviders, llmprovider.Bootstrap{
\t\t\tProvider: provider, BaseURL: configured.BaseURL, DefaultModel: configured.Model,
\t\t\tJSONMode: configured.JSONMode, Timeout: configured.Timeout,
\t\t})
\t}
\tdefaultProvider := cfg.LLM.Provider
\tdefaultModel := cfg.LLM.Model
\tif defaultProvider == "" || defaultProvider == planningagent.LocalFakeProviderName {
\t\tdefaultProvider = planningagent.LocalFakeProviderName
\t\tdefaultModel = planningagent.LocalFakeModelName
\t}
\tproviderConfigRepository, err := llmprovider.NewRepository(db)
\tif err != nil {
\t\treturn err
\t}
\tproviderService, err := llmprovider.NewService(
\t\tproviderConfigRepository,
\t\tproviderRegistry,
\t\tcredentialStore,
\t\tdefaultProvider,
\t\tbootstrapProviders,
\t)
\tif err != nil {
\t\treturn err
\t}
\tif err := providerService.Load(runtimeCtx); err != nil {
\t\treturn err
\t}
\tproviderCatalog := providerService''',
)

regex_replace(
    "cmd/api/main.go",
    r'''\tcredentialStore, err := credentials\.NewOSStore\("orkoda"\)
\tif err != nil \{
\t\treturn err
\t\}
\tgithubPublisher, err := publication\.NewGitHubPublisher\(credentialStore\)''',
    r'''\tgithubPublisher, err := publication.NewGitHubPublisher(credentialStore)''',
)

replace(
    "cmd/api/main.go",
    '''\t\t\t\tLLMProviders:        providerCatalog,
\t\t\t\tLLMPolicy:           llmGateway,''',
    '''\t\t\t\tLLMProviders:        providerCatalog,
\t\t\t\tLLMProviderAdmin:    providerService,
\t\t\t\tLLMPolicy:           llmGateway,''',
)
