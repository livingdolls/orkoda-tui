# AI provider setup

The normal setup path is entirely inside the TUI.

1. Start the daemon with `make api`.
2. Start the TUI with `make tui`.
3. Open **Settings** and press `N`.
4. Cycle DeepSeek, OpenAI, and Custom presets with `Ctrl+P`.
5. Review the provider name, base URL, model, structured-output mode, and timeout.
6. Paste the API key and save with `Ctrl+S`.
7. Select the provider and press `T` to run a small connection test.
8. Open **Agents** and assign the provider/model to Planner, Executor, or Reviewer.

A saved provider is registered in the running daemon immediately; no restart is required. Existing workflow runs retain their immutable provider/model snapshots.

## Credential boundary

Provider metadata is persisted in SQLite, but API keys are not. Orkoda first uses the operating-system keychain. On systems where the keychain command is unavailable, it falls back to `${ORKODA_DATA_DIR}/credentials.json`, created with owner-only permissions. Provider APIs, diagnostics, and activity events expose only whether a credential is stored.

The current terminal input component does not provide password masking. The key is therefore visible only while it is being typed. After a successful save, the TUI clears the field and the daemon never returns the value.

## Advanced environment bootstrap

Environment configuration remains supported for CI, containers, and centrally managed deployments:

```text
ORKODA_LLM_PROVIDER=provider-name
ORKODA_LLM_PROVIDERS_JSON=[{"name":"provider-name","base_url":"https://provider.example/v1","api_key_env":"PROVIDER_API_KEY","model":"provider-model"}]
PROVIDER_API_KEY=...
```

A TUI-managed record with the same provider name overrides the environment provider at runtime. Removing that TUI record restores the environment-backed provider.
