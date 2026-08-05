package llmprovider

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
		repository:         repository,
		registry:           registry,
		credentials:        credentialStore,
		defaultProvider:    strings.ToLower(strings.TrimSpace(defaultProvider)),
		managed:            map[string]Record{},
		managedCredentials: map[string]bool{},
		bootstrap:          map[string]Bootstrap{},
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
			// A persisted TUI override must never silently fall through to an
			// environment provider with the same name when its credential is gone.
			s.registry.Remove(name)
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
		Temperature:     0,
	})
	if err != nil {
		return TestResult{}, fmt.Errorf("provider test failed: %w", err)
	}
	preview := strings.TrimSpace(response.Content)
	if len(preview) > 120 {
		preview = preview[:120]
	}
	return TestResult{
		Provider:        name,
		Model:           response.Model,
		LatencyMS:       time.Since(started).Milliseconds(),
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
		if sensitiveHeader(key) {
			return Record{}, fmt.Errorf("%w: sensitive credentials must use the API key field, not header %s", ErrInvalid, key)
		}
		if key != "" && value != "" {
			cleanHeaders[key] = value
		}
	}
	return Record{
		Name:         name,
		BaseURL:      baseURL,
		DefaultModel: model,
		JSONMode:     jsonMode,
		Timeout:      timeout,
		Headers:      cleanHeaders,
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
		Name:         item.Name,
		BaseURL:      item.BaseURL,
		APIKey:       apiKey,
		DefaultModel: item.DefaultModel,
		Headers:      item.Headers,
		Timeout:      item.Timeout,
		JSONMode:     openaicompat.JSONMode(item.JSONMode),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return provider, nil
}

func sensitiveHeader(name string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(name), "_", "-"), " ", "-"))
	if normalized == "authorization" || normalized == "proxy-authorization" || normalized == "x-api-key" {
		return true
	}
	return strings.Contains(normalized, "token") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "api-key")
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
