# Multi-provider agents

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
