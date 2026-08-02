import { describe, expect, test } from "bun:test"

import { getLLMPolicy, type LLMProviderFetch, listLLMProviders } from "./llm-providers"

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
            },
            {
              name: "local-fake",
              default_model: "local-fake-planner-v1",
              configured: true,
              structured_output: true,
              default: false,
            },
          ],
        }),
        { status: 200 },
      )
    }

    const providers = await listLLMProviders(fetcher)
    expect(requestedURL).toEndWith("/api/v1/llm/providers")
    expect(providers).toHaveLength(2)
    expect(providers[0]?.name).toBe("openrouter")
    expect(providers[0]?.default).toBe(true)
    expect(JSON.stringify(providers)).not.toContain("api_key")
  })

  test("loads the read-only execution policy", async () => {
    let requestedURL = ""
    const fetcher: LLMProviderFetch = async (input) => {
      requestedURL = String(input)
      return new Response(
        JSON.stringify({
          data: {
            attempt_timeout_ms: 45000,
            max_wall_clock_ms: 120000,
            max_attempts: 3,
            initial_backoff_ms: 500,
            max_backoff_ms: 8000,
            jitter: 0.2,
            fallbacks: [{ provider: "local-fake", model: "local-fake-planner-v1" }],
            budget: {
              max_input_tokens: 50000,
              max_output_tokens: 8000,
              max_total_tokens: 60000,
            },
          },
        }),
        { status: 200 },
      )
    }

    const policy = await getLLMPolicy(fetcher)
    expect(requestedURL).toEndWith("/api/v1/llm/policy")
    expect(policy.max_attempts).toBe(3)
    expect(policy.fallbacks[0]?.provider).toBe("local-fake")
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
