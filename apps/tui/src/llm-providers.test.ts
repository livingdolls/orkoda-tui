import { describe, expect, test } from "bun:test"

import { listLLMProviders, type LLMProviderFetch } from "./llm-providers"

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

  test("surfaces daemon errors", async () => {
    const fetcher: LLMProviderFetch = async () =>
      new Response(JSON.stringify({ error: { message: "provider catalog unavailable" } }), {
        status: 503,
      })

    await expect(listLLMProviders(fetcher)).rejects.toThrow("provider catalog unavailable")
  })
})
