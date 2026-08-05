import { describe, expect, test } from "bun:test"

import {
  deleteLLMProvider,
  getLLMPolicy,
  type LLMProviderFetch,
  listLLMProviders,
  saveLLMProvider,
  testLLMProvider,
} from "./llm-providers"

describe("LLM provider API client", () => {
  test("lists safe provider metadata", async () => {
    let requestedURL = ""
    const fetcher: LLMProviderFetch = async (input) => {
      requestedURL = String(input)
      return new Response(
        JSON.stringify({
          data: [
            {
              name: "openrouter",
              default_model: "example/model",
              configured: true,
              structured_output: true,
              default: true,
              base_url: "https://provider.example/v1",
              json_mode: "json_schema",
              timeout_ms: 60000,
              credential_stored: true,
              source: "tui",
              editable: true,
              deletable: true,
            },
          ],
        }),
        { status: 200 },
      )
    }

    const providers = await listLLMProviders(fetcher)
    expect(requestedURL).toEndWith("/api/v1/llm/providers")
    expect(providers[0]?.name).toBe("openrouter")
    expect(providers[0]?.credential_stored).toBe(true)
    expect(JSON.stringify(providers)).not.toContain("api_key")
  })

  test("creates, tests, and deletes a provider", async () => {
    const calls: Array<{ url: string; method: string; body: string }> = []
    const fetcher: LLMProviderFetch = async (input, init) => {
      calls.push({
        url: String(input),
        method: init?.method ?? "GET",
        body: typeof init?.body === "string" ? init.body : "",
      })
      if (init?.method === "DELETE") return new Response(null, { status: 204 })
      if (String(input).endsWith("/test")) {
        return new Response(
          JSON.stringify({
            data: {
              provider: "deepseek",
              model: "deepseek-v4-flash",
              latency_ms: 42,
              response_preview: "OK",
            },
          }),
          { status: 200 },
        )
      }
      return new Response(
        JSON.stringify({
          data: {
            name: "deepseek",
            default_model: "deepseek-v4-flash",
            configured: true,
            structured_output: true,
            default: false,
            credential_stored: true,
            source: "tui",
            editable: true,
            deletable: true,
          },
        }),
        { status: 200 },
      )
    }

    const saved = await saveLLMProvider(
      "deepseek",
      {
        base_url: "https://api.deepseek.com",
        default_model: "deepseek-v4-flash",
        api_key: "test-provider-value",
        json_mode: "json_object",
      },
      fetcher,
    )
    expect(saved.configured).toBe(true)
    expect(calls[0]?.method).toBe("PUT")
    expect(calls[0]?.body).toContain("test-provider-value")

    const result = await testLLMProvider("deepseek", fetcher)
    expect(result.response_preview).toBe("OK")
    expect(calls[1]?.method).toBe("POST")

    await deleteLLMProvider("deepseek", fetcher)
    expect(calls[2]?.method).toBe("DELETE")
  })

  test("loads the execution and safety policy", async () => {
    const fetcher: LLMProviderFetch = async () =>
      new Response(
        JSON.stringify({
          data: {
            attempt_timeout_ms: 45000,
            max_wall_clock_ms: 120000,
            max_attempts: 3,
            initial_backoff_ms: 500,
            max_backoff_ms: 8000,
            jitter: 0.2,
            fallbacks: [{ provider: "local-fake", model: "local-fake-planner-v1" }],
            budget: { max_input_tokens: 50000, max_output_tokens: 8000, max_total_tokens: 60000 },
            redaction_mode: "strict",
            structured_validation: true,
            max_repair_attempts: 1,
            max_structured_response_bytes: 1048576,
          },
        }),
        { status: 200 },
      )

    const policy = await getLLMPolicy(fetcher)
    expect(policy.max_attempts).toBe(3)
    expect(policy.budget.max_total_tokens).toBe(60000)
  })

  test("surfaces daemon errors", async () => {
    const fetcher: LLMProviderFetch = async () =>
      new Response(JSON.stringify({ error: { message: "provider catalog unavailable" } }), {
        status: 503,
      })
    await expect(listLLMProviders(fetcher)).rejects.toThrow("provider catalog unavailable")
  })
})
