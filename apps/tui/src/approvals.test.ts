import { describe, expect, test } from "bun:test"

import { type ApprovalFetch, listApprovalDecisions, submitApprovalDecision } from "./approvals"

describe("approval API client", () => {
  test("lists decisions and posts bound approval", async () => {
    const requests: Array<{ url: string; init?: RequestInit }> = []
    const fetcher: ApprovalFetch = async (input, init) => {
      const url = String(input)
      requests.push({ url, init })
      if (init?.method === "POST") {
        return new Response(
          JSON.stringify({
            data: {
              decision: { id: "decision-1", decision: "APPROVE", status: "APPLIED" },
              workflow: { id: "workflow-1", status: "APPROVED" },
            },
          }),
          { status: 200 },
        )
      }
      return new Response(
        JSON.stringify({ data: [{ id: "decision-1", decision: "APPROVE", status: "APPLIED" }] }),
        { status: 200 },
      )
    }

    const decisions = await listApprovalDecisions("workflow-1", fetcher)
    const outcome = await submitApprovalDecision(
      "workflow-1",
      "APPROVE",
      {
        expected_version: 8,
        execution_version: 1,
        base_commit_sha: "abc123",
        patch_checksum: "sha256:patch",
        note: "Reviewed locally.",
        review_override: false,
      },
      fetcher,
    )

    expect(decisions[0]?.id).toBe("decision-1")
    expect(outcome.workflow.status).toBe("APPROVED")
    expect(requests[0]?.url).toEndWith("/api/v1/jobs/workflow-1/decisions")
    expect(requests[1]?.url).toEndWith("/api/v1/jobs/workflow-1/approve")
    expect(requests[1]?.init?.method).toBe("POST")
    expect(String(requests[1]?.init?.body)).toContain('"patch_checksum":"sha256:patch"')
  })

  test("surfaces approval conflicts", async () => {
    const fetcher: ApprovalFetch = async () =>
      new Response(JSON.stringify({ error: { message: "approval binding mismatch" } }), {
        status: 409,
      })

    await expect(
      submitApprovalDecision(
        "workflow-1",
        "REJECT",
        {
          expected_version: 8,
          execution_version: 1,
          base_commit_sha: "abc123",
          patch_checksum: "sha256:stale",
          note: "Reject stale patch.",
          review_override: false,
        },
        fetcher,
      ),
    ).rejects.toThrow("approval binding mismatch")
  })
})
