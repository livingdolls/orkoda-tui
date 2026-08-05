from pathlib import Path

root = Path('.')


def replace_once(path: Path, old: str, new: str) -> None:
    text = path.read_text()
    if old not in text:
        raise SystemExit(f'marker not found in {path}: {old[:100]!r}')
    path.write_text(text.replace(old, new, 1))


def replace_between(path: Path, start: str, end: str, replacement: str) -> None:
    text = path.read_text()
    start_index = text.find(start)
    if start_index < 0:
        raise SystemExit(f'start marker not found in {path}: {start!r}')
    end_index = text.find(end, start_index)
    if end_index < 0:
        raise SystemExit(f'end marker not found in {path}: {end!r}')
    path.write_text(text[:start_index] + replacement + text[end_index:])


llm_path = root / 'internal/config/llm.go'
replace_once(
    llm_path,
    '''type LLMFallbackConfig struct {
\tProvider string `json:"provider"`
\tModel    string `json:"model"`
}
''',
    '''type LLMFallbackConfig struct {
\tProvider string `json:"provider"`
\tModel    string `json:"model"`
}

type LLMProviderConfig struct {
\tName     string
\tBaseURL  string
\tAPIKey   string
\tModel    string
\tJSONMode string
\tTimeout  time.Duration
\tHeaders  map[string]string
}

type rawLLMProviderConfig struct {
\tName      string            `json:"name"`
\tBaseURL   string            `json:"base_url"`
\tAPIKey    string            `json:"api_key,omitempty"`
\tAPIKeyEnv string            `json:"api_key_env,omitempty"`
\tModel     string            `json:"model"`
\tJSONMode  string            `json:"json_mode,omitempty"`
\tTimeout   string            `json:"timeout,omitempty"`
\tHeaders   map[string]string `json:"headers,omitempty"`
}
''',
)
replace_once(
    llm_path,
    '''\tFallbacks                  []LLMFallbackConfig
\tRedactionMode              string
''',
    '''\tFallbacks                  []LLMFallbackConfig
\tProviders                  []LLMProviderConfig
\tRedactionMode              string
''',
)

new_load = r'''func loadLLMConfig() (LLMConfig, error) {
\ttimeout, err := durationFromEnv("ORKODA_LLM_TIMEOUT", defaultLLMTimeout)
\tif err != nil {
\t\treturn LLMConfig{}, err
\t}
\tattemptTimeout, err := durationFromEnv("ORKODA_LLM_ATTEMPT_TIMEOUT", defaultAttemptTimeout)
\tif err != nil {
\t\treturn LLMConfig{}, err
\t}
\tmaxWallClock, err := durationFromEnv("ORKODA_LLM_MAX_WALL_CLOCK", defaultMaxWallClock)
\tif err != nil {
\t\treturn LLMConfig{}, err
\t}
\tinitialBackoff, err := durationFromEnv("ORKODA_LLM_BACKOFF_INITIAL", defaultInitialBackoff)
\tif err != nil {
\t\treturn LLMConfig{}, err
\t}
\tmaxBackoff, err := durationFromEnv("ORKODA_LLM_BACKOFF_MAX", defaultMaxBackoff)
\tif err != nil {
\t\treturn LLMConfig{}, err
\t}
\tmaxAttempts, err := positiveIntFromEnv("ORKODA_LLM_MAX_ATTEMPTS", defaultMaxAttempts)
\tif err != nil {
\t\treturn LLMConfig{}, err
\t}
\tbackoffJitter, err := fractionFromEnv("ORKODA_LLM_BACKOFF_JITTER", defaultBackoffJitter)
\tif err != nil {
\t\treturn LLMConfig{}, err
\t}
\tmaxInputTokens, err := nonNegativeIntFromEnv("ORKODA_LLM_MAX_INPUT_TOKENS", defaultMaxInputTokens)
\tif err != nil {
\t\treturn LLMConfig{}, err
\t}
\tmaxOutputTokens, err := nonNegativeIntFromEnv("ORKODA_LLM_MAX_OUTPUT_TOKENS", defaultMaxOutputTokens)
\tif err != nil {
\t\treturn LLMConfig{}, err
\t}
\tmaxTotalTokens, err := nonNegativeIntFromEnv("ORKODA_LLM_MAX_TOTAL_TOKENS", defaultMaxTotalTokens)
\tif err != nil {
\t\treturn LLMConfig{}, err
\t}
\tmaxRepairAttempts, err := nonNegativeIntFromEnv("ORKODA_LLM_MAX_REPAIR_ATTEMPTS", defaultLLMMaxRepairAttempts)
\tif err != nil {
\t\treturn LLMConfig{}, err
\t}
\tmaxStructuredResponseBytes, err := positiveIntFromEnv(
\t\t"ORKODA_LLM_MAX_STRUCTURED_RESPONSE_BYTES",
\t\tdefaultLLMMaxStructuredResponseBytes,
\t)
\tif err != nil {
\t\treturn LLMConfig{}, err
\t}
\theaders, err := stringMapFromEnv("ORKODA_LLM_HEADERS_JSON")
\tif err != nil {
\t\treturn LLMConfig{}, err
\t}
\tfallbacks, err := fallbackConfigFromEnv("ORKODA_LLM_FALLBACKS_JSON")
\tif err != nil {
\t\treturn LLMConfig{}, err
\t}
\tjsonMode := strings.ToLower(strings.TrimSpace(stringFromEnv("ORKODA_LLM_JSON_MODE", defaultLLMJSONMode)))
\tproviders, err := providerConfigsFromEnv("ORKODA_LLM_PROVIDERS_JSON", timeout, jsonMode, headers)
\tif err != nil {
\t\treturn LLMConfig{}, err
\t}

\tconfiguredDefault := strings.ToLower(strings.TrimSpace(os.Getenv("ORKODA_LLM_PROVIDER")))
\tproviderName := configuredDefault
\tif providerName == "" {
\t\tif len(providers) > 0 {
\t\t\tproviderName = providers[0].Name
\t\t} else {
\t\t\tproviderName = defaultLLMProvider
\t\t}
\t}
\tlegacyBaseURL := strings.TrimSpace(os.Getenv("ORKODA_LLM_BASE_URL"))
\tlegacyAPIKey := strings.TrimSpace(os.Getenv("ORKODA_LLM_API_KEY"))
\tlegacyModel := strings.TrimSpace(os.Getenv("ORKODA_LLM_MODEL"))
\tif len(providers) == 0 && providerName != defaultLLMProvider {
\t\tif legacyBaseURL == "" {
\t\t\treturn LLMConfig{}, fmt.Errorf("ORKODA_LLM_BASE_URL is required when ORKODA_LLM_PROVIDER is %s", providerName)
\t\t}
\t\tif legacyAPIKey == "" {
\t\t\treturn LLMConfig{}, fmt.Errorf("ORKODA_LLM_API_KEY is required when ORKODA_LLM_PROVIDER is %s", providerName)
\t\t}
\t\tif legacyModel == "" {
\t\t\treturn LLMConfig{}, fmt.Errorf("ORKODA_LLM_MODEL is required when ORKODA_LLM_PROVIDER is %s", providerName)
\t\t}
\t\tproviders = []LLMProviderConfig{{
\t\t\tName: providerName, BaseURL: legacyBaseURL, APIKey: legacyAPIKey,
\t\t\tModel: legacyModel, JSONMode: jsonMode, Timeout: timeout, Headers: headers,
\t\t}}
\t}

\tdefaultModel := legacyModel
\tdefaultBaseURL := legacyBaseURL
\tdefaultAPIKey := legacyAPIKey
\tregistered := map[string]struct{}{defaultLLMProvider: {}}
\tfor _, provider := range providers {
\t\tregistered[provider.Name] = struct{}{}
\t\tif provider.Name == providerName {
\t\t\tif defaultModel == "" {
\t\t\t\tdefaultModel = provider.Model
\t\t\t}
\t\t\tdefaultBaseURL = provider.BaseURL
\t\t\tdefaultAPIKey = provider.APIKey
\t\t\tjsonMode = provider.JSONMode
\t\t\ttimeout = provider.Timeout
\t\t\theaders = provider.Headers
\t\t}
\t}
\tif _, exists := registered[providerName]; !exists {
\t\treturn LLMConfig{}, fmt.Errorf("default LLM provider %s is not registered", providerName)
\t}
\tif providerName != defaultLLMProvider && defaultModel == "" {
\t\treturn LLMConfig{}, fmt.Errorf("default LLM provider %s requires a model", providerName)
\t}

\tconfig := LLMConfig{
\t\tProvider: providerName, BaseURL: defaultBaseURL, APIKey: defaultAPIKey,
\t\tModel: defaultModel, JSONMode: jsonMode, Timeout: timeout, Headers: headers,
\t\tAttemptTimeout: attemptTimeout, MaxWallClock: maxWallClock, MaxAttempts: maxAttempts,
\t\tInitialBackoff: initialBackoff, MaxBackoff: maxBackoff, BackoffJitter: backoffJitter,
\t\tMaxInputTokens: maxInputTokens, MaxOutputTokens: maxOutputTokens,
\t\tMaxTotalTokens: maxTotalTokens, Fallbacks: fallbacks, Providers: providers,
\t\tRedactionMode: strings.ToLower(strings.TrimSpace(stringFromEnv("ORKODA_LLM_REDACTION_MODE", defaultLLMRedactionMode))),
\t\tMaxRepairAttempts: maxRepairAttempts, MaxStructuredResponseBytes: maxStructuredResponseBytes,
\t}
\tif config.InitialBackoff > config.MaxBackoff {
\t\treturn LLMConfig{}, fmt.Errorf("ORKODA_LLM_BACKOFF_MAX must not be smaller than ORKODA_LLM_BACKOFF_INITIAL")
\t}
\tswitch config.RedactionMode {
\tcase "strict", "report", "off":
\tdefault:
\t\treturn LLMConfig{}, fmt.Errorf("ORKODA_LLM_REDACTION_MODE must be strict, report, or off")
\t}
\tseenFallbacks := make(map[string]struct{}, len(config.Fallbacks))
\tfor _, fallback := range config.Fallbacks {
\t\tif fallback.Provider == config.Provider {
\t\t\treturn LLMConfig{}, fmt.Errorf("fallback provider %s must differ from ORKODA_LLM_PROVIDER", fallback.Provider)
\t\t}
\t\tif _, exists := registered[fallback.Provider]; !exists {
\t\t\treturn LLMConfig{}, fmt.Errorf("fallback provider %s is not registered", fallback.Provider)
\t\t}
\t\tkey := fallback.Provider + "\\x00" + fallback.Model
\t\tif _, exists := seenFallbacks[key]; exists {
\t\t\treturn LLMConfig{}, fmt.Errorf("duplicate LLM fallback target %s/%s", fallback.Provider, fallback.Model)
\t\t}
\t\tseenFallbacks[key] = struct{}{}
\t}
\treturn config, nil
}

'''
replace_between(llm_path, 'func loadLLMConfig() (LLMConfig, error) {', 'func stringMapFromEnv', new_load)

provider_helper = r'''func providerConfigsFromEnv(
\tkey string,
\tdefaultTimeout time.Duration,
\tdefaultJSONMode string,
\tdefaultHeaders map[string]string,
) ([]LLMProviderConfig, error) {
\tvalue := strings.TrimSpace(os.Getenv(key))
\tif value == "" {
\t\treturn nil, nil
\t}
\tvar raw []rawLLMProviderConfig
\tif err := json.Unmarshal([]byte(value), &raw); err != nil {
\t\treturn nil, fmt.Errorf("parse %s: %w", key, err)
\t}
\tif len(raw) == 0 {
\t\treturn nil, fmt.Errorf("%s must contain at least one provider", key)
\t}
\tseen := make(map[string]struct{}, len(raw))
\tproviders := make([]LLMProviderConfig, 0, len(raw))
\tfor index, item := range raw {
\t\tname := strings.ToLower(strings.TrimSpace(item.Name))
\t\tif name == "" || name == defaultLLMProvider {
\t\t\treturn nil, fmt.Errorf("%s entry %d requires a non-local provider name", key, index)
\t\t}
\t\tif _, exists := seen[name]; exists {
\t\t\treturn nil, fmt.Errorf("%s contains duplicate provider %s", key, name)
\t\t}
\t\tseen[name] = struct{}{}
\t\tbaseURL := strings.TrimSpace(item.BaseURL)
\t\tmodel := strings.TrimSpace(item.Model)
\t\tif baseURL == "" || model == "" {
\t\t\treturn nil, fmt.Errorf("%s provider %s requires base_url and model", key, name)
\t\t}
\t\tapiKey := strings.TrimSpace(item.APIKey)
\t\tapiKeyEnv := strings.TrimSpace(item.APIKeyEnv)
\t\tif apiKey == "" && apiKeyEnv != "" {
\t\t\tapiKey = strings.TrimSpace(os.Getenv(apiKeyEnv))
\t\t}
\t\tif apiKey == "" {
\t\t\treturn nil, fmt.Errorf("%s provider %s requires api_key or a populated api_key_env", key, name)
\t\t}
\t\ttimeout := defaultTimeout
\t\tif strings.TrimSpace(item.Timeout) != "" {
\t\t\tparsed, err := time.ParseDuration(strings.TrimSpace(item.Timeout))
\t\t\tif err != nil || parsed <= 0 {
\t\t\t\treturn nil, fmt.Errorf("%s provider %s has invalid timeout", key, name)
\t\t\t}
\t\t\ttimeout = parsed
\t\t}
\t\tjsonMode := strings.ToLower(strings.TrimSpace(item.JSONMode))
\t\tif jsonMode == "" {
\t\t\tjsonMode = defaultJSONMode
\t\t}
\t\tswitch jsonMode {
\t\tcase "json_schema", "json_object", "prompt_only":
\t\tdefault:
\t\t\treturn nil, fmt.Errorf("%s provider %s has invalid json_mode", key, name)
\t\t}
\t\theaders := item.Headers
\t\tif headers == nil {
\t\t\theaders = defaultHeaders
\t\t}
\t\tproviders = append(providers, LLMProviderConfig{
\t\t\tName: name, BaseURL: baseURL, APIKey: apiKey, Model: model,
\t\t\tJSONMode: jsonMode, Timeout: timeout, Headers: headers,
\t\t})
\t}
\treturn providers, nil
}

'''
replace_once(llm_path, 'func stringMapFromEnv', provider_helper + 'func stringMapFromEnv')

main_path = root / 'cmd/api/main.go'
new_provider_setup = '''\tlocalPlanningProvider := planningagent.NewLocalFakeProvider()
\tproviderRegistry, err := llm.NewRegistry(localPlanningProvider)
\tif err != nil {
\t\treturn err
\t}
\tfor _, configured := range cfg.LLM.Providers {
\t\tprovider, err := openaicompat.New(openaicompat.Config{
\t\t\tName: configured.Name, BaseURL: configured.BaseURL, APIKey: configured.APIKey,
\t\t\tDefaultModel: configured.Model, Headers: configured.Headers,
\t\t\tTimeout: configured.Timeout, JSONMode: openaicompat.JSONMode(configured.JSONMode),
\t\t})
\t\tif err != nil {
\t\t\treturn fmt.Errorf("configure LLM provider %s: %w", configured.Name, err)
\t\t}
\t\tif err := providerRegistry.Register(provider); err != nil {
\t\t\treturn err
\t\t}
\t}
\tdefaultProvider := cfg.LLM.Provider
\tdefaultModel := cfg.LLM.Model
\tif defaultProvider == "" || defaultProvider == planningagent.LocalFakeProviderName {
\t\tdefaultProvider = planningagent.LocalFakeProviderName
\t\tdefaultModel = planningagent.LocalFakeModelName
\t}
\tproviderCatalog := llm.NewCatalog(providerRegistry, defaultProvider)

'''
replace_between(main_path, '\tlocalPlanningProvider := planningagent.NewLocalFakeProvider()', '\tfallbacks := make([]llm.FallbackTarget', new_provider_setup)

agent_path = root / 'internal/agentconfig/repository.go'
replace_once(
    agent_path,
    '''\tfor index, role := range roles {
\t\tif configs[index].Role != role || policies[index].Role != role {
\t\t\treturn fmt.Errorf("%w: roles must include PLANNER, EXECUTOR, and REVIEWER exactly once", ErrInvalidSettings)
\t\t}
\t\tif err := validateAgent(configs[index]); err != nil {
\t\t\treturn err
\t\t}
\t\tif err := validatePolicy(policies[index]); err != nil {
\t\t\treturn err
\t\t}
\t}
\treturn nil
''',
    '''\tfor index, role := range roles {
\t\tif configs[index].Role != role || policies[index].Role != role {
\t\t\treturn fmt.Errorf("%w: roles must include PLANNER, EXECUTOR, and REVIEWER exactly once", ErrInvalidSettings)
\t\t}
\t\tif err := validateAgent(configs[index]); err != nil {
\t\t\treturn err
\t\t}
\t\tif err := validatePolicy(policies[index]); err != nil {
\t\t\treturn err
\t\t}
\t}
\texecutor := configs[roleIndex(RoleExecutor)]
\treviewer := configs[roleIndex(RoleReviewer)]
\tif executor.Enabled && reviewer.Enabled && executor.Provider != "" && reviewer.Provider != "" &&
\t\texecutor.Provider == reviewer.Provider && executor.Model == reviewer.Model {
\t\treturn fmt.Errorf("%w: executor and reviewer must not use the same explicit provider/model", ErrInvalidSettings)
\t}
\treturn nil
''',
)

(root / 'internal/config/llm_multi_provider_test.go').write_text(r'''package config

import (
\t"strings"
\t"testing"
)

func TestLoadLLMConfigRegistersMultipleProviders(t *testing.T) {
\tt.Setenv("DEEPSEEK_API_KEY", strings.Repeat("d", 32))
\tt.Setenv("OPENAI_API_KEY", strings.Repeat("o", 32))
\tt.Setenv("ORKODA_LLM_PROVIDER", "deepseek")
\tt.Setenv("ORKODA_LLM_BASE_URL", "")
\tt.Setenv("ORKODA_LLM_API_KEY", "")
\tt.Setenv("ORKODA_LLM_MODEL", "")
\tt.Setenv("ORKODA_LLM_PROVIDERS_JSON", `[
\t\t{"name":"deepseek","base_url":"https://api.deepseek.example/v1","api_key_env":"DEEPSEEK_API_KEY","model":"deepseek-coder"},
\t\t{"name":"openai","base_url":"https://api.openai.example/v1","api_key_env":"OPENAI_API_KEY","model":"review-model","timeout":"75s"}
\t]`)
\tt.Setenv("ORKODA_LLM_FALLBACKS_JSON", `[{"provider":"openai","model":"review-model"}]`)

\tconfig, err := loadLLMConfig()
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif config.Provider != "deepseek" || config.Model != "deepseek-coder" {
\t\tt.Fatalf("unexpected default: %s/%s", config.Provider, config.Model)
\t}
\tif len(config.Providers) != 2 {
\t\tt.Fatalf("expected two providers, got %d", len(config.Providers))
\t}
\tif config.Providers[1].Timeout.String() != "1m15s" {
\t\tt.Fatalf("unexpected provider timeout: %s", config.Providers[1].Timeout)
\t}
}

func TestLoadLLMConfigRejectsUnregisteredFallback(t *testing.T) {
\tt.Setenv("DEEPSEEK_API_KEY", strings.Repeat("d", 32))
\tt.Setenv("ORKODA_LLM_PROVIDER", "deepseek")
\tt.Setenv("ORKODA_LLM_PROVIDERS_JSON", `[{"name":"deepseek","base_url":"https://api.deepseek.example/v1","api_key_env":"DEEPSEEK_API_KEY","model":"deepseek-coder"}]`)
\tt.Setenv("ORKODA_LLM_FALLBACKS_JSON", `[{"provider":"missing","model":"model"}]`)
\tif _, err := loadLLMConfig(); err == nil || !strings.Contains(err.Error(), "not registered") {
\t\tt.Fatalf("expected unregistered fallback error, got %v", err)
\t}
}
''')

(root / 'internal/agentconfig/separation_test.go').write_text(r'''package agentconfig

import (
\t"errors"
\t"testing"
)

func TestValidateAggregateRejectsSameExecutorAndReviewer(t *testing.T) {
\tagents := defaultAgents()
\tagents[1].Provider, agents[1].Model = "shared", "same-model"
\tagents[2].Provider, agents[2].Model = "shared", "same-model"
\tif err := validateAggregate(agents, defaultPolicies()); !errors.Is(err, ErrInvalidSettings) {
\t\tt.Fatalf("expected invalid settings, got %v", err)
\t}
}

func TestValidateAggregateAllowsDifferentReviewModel(t *testing.T) {
\tagents := defaultAgents()
\tagents[1].Provider, agents[1].Model = "shared", "executor-model"
\tagents[2].Provider, agents[2].Model = "shared", "review-model"
\tif err := validateAggregate(agents, defaultPolicies()); err != nil {
\t\tt.Fatal(err)
\t}
}
''')

(root / '.env.example').write_text('''ORKODA_ENV=development
ORKODA_LOG_LEVEL=debug
ORKODA_API_HOST=127.0.0.1
ORKODA_API_PORT=8181
ORKODA_API_TOKEN=
# Defaults to ${ORKODA_DATA_DIR}/api.token when unset.
ORKODA_API_TOKEN_FILE=.orkoda/api.token
ORKODA_SHUTDOWN_TIMEOUT=10s
ORKODA_DATA_DIR=.orkoda
ORKODA_DATABASE_PATH=.orkoda/orkoda.db
ORKODA_ARTIFACT_DIR=.orkoda/artifacts
ORKODA_SANDBOX_MODE=docker
ORKODA_SANDBOX_IMAGE=orkoda-sandbox:local
ORKODA_ALLOW_UNSANDBOXED_CHECKS=false

# Multi-provider setup. Keep credentials in dedicated environment variables;
# do not place real API keys in this JSON or commit them to the repository.
DEEPSEEK_API_KEY=
OPENAI_API_KEY=
ORKODA_LLM_PROVIDER=deepseek
ORKODA_LLM_PROVIDERS_JSON=[{"name":"deepseek","base_url":"https://provider.example/v1","api_key_env":"DEEPSEEK_API_KEY","model":"executor-model"},{"name":"openai","base_url":"https://provider.example/v1","api_key_env":"OPENAI_API_KEY","model":"reviewer-model"}]

# Legacy single-provider configuration remains supported when
# ORKODA_LLM_PROVIDERS_JSON is empty.
ORKODA_LLM_BASE_URL=
ORKODA_LLM_API_KEY=
ORKODA_LLM_MODEL=
ORKODA_LLM_JSON_MODE=json_schema
ORKODA_LLM_TIMEOUT=60s
ORKODA_LLM_HEADERS_JSON={}

ORKODA_LLM_ATTEMPT_TIMEOUT=45s
ORKODA_LLM_MAX_WALL_CLOCK=2m
ORKODA_LLM_MAX_ATTEMPTS=3
ORKODA_LLM_BACKOFF_INITIAL=500ms
ORKODA_LLM_BACKOFF_MAX=8s
ORKODA_LLM_BACKOFF_JITTER=0.2
ORKODA_LLM_MAX_INPUT_TOKENS=50000
ORKODA_LLM_MAX_OUTPUT_TOKENS=8000
ORKODA_LLM_MAX_TOTAL_TOKENS=60000
ORKODA_LLM_FALLBACKS_JSON=[]
ORKODA_LLM_REDACTION_MODE=strict
ORKODA_LLM_MAX_REPAIR_ATTEMPTS=1
ORKODA_LLM_MAX_STRUCTURED_RESPONSE_BYTES=1048576
''')

(root / 'docs/multi-provider-agents.md').write_text('''# Multi-provider agents

Orkoda can register several OpenAI-compatible providers in one daemon process. This allows an Executor and Reviewer to use independent models while keeping one durable workflow.

```env
DEEPSEEK_API_KEY=...
OPENAI_API_KEY=...
ORKODA_LLM_PROVIDER=deepseek
ORKODA_LLM_PROVIDERS_JSON=[
  {"name":"deepseek","base_url":"https://provider.example/v1","api_key_env":"DEEPSEEK_API_KEY","model":"executor-model"},
  {"name":"openai","base_url":"https://provider.example/v1","api_key_env":"OPENAI_API_KEY","model":"reviewer-model"}
]
```

The JSON is normally written on one line in `.env`. `api_key_env` names an environment variable; the credential itself is not stored in the JSON. The legacy single-provider variables remain supported.

After the daemon starts, open **Agents**, select the project, set the Executor and Reviewer provider/model, and save the versioned settings. Explicitly assigning the exact same provider and model to both roles is rejected. Reviewer filesystem access remains read-only and its tool policy cannot contain mutation tools.
''')
