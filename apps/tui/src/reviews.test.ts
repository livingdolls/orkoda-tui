import { describe, expect, test } from "bun:test"

import { listReviewIssues, listReviews, type ReviewFetch } from "./reviews"

describe("review API client", () => {
  test("loads review runs and issues", async () => {
    const urls: string[] = []
    const fetcher: ReviewFetch = async (input) => {
      const url = String(input)
      urls.push(url)
      if (url.endsWith("/issues")) {
        return new Response(
          JSON.stringify({
            data: [
              {
                id: "issue-1",
                review_run_id: "review-1",
                severity: "HIGH",
                blocking: true,
              },
            ],
          }),
          { status: 200 },
        )
      }
      return new Response(
        JSON.stringify({
          data: [
            {
              id: "review-1",
              workflow_job_id: "workflow-1",
              status: "COMPLETED",
              verdict: "REQUEST_REVISION",
            },
          ],
        }),
        { status: 200 },
      )
    }

    const reviews = await listReviews("workflow-1", fetcher)
    const issues = await listReviewIssues("review-1", fetcher)

    expect(reviews[0]?.id).toBe("review-1")
    expect(issues[0]?.id).toBe("issue-1")
    expect(urls[0]).toEndWith("/api/v1/jobs/workflow-1/reviews")
    expect(urls[1]).toEndWith("/api/v1/reviews/review-1/issues")
  })

  test("surfaces daemon errors", async () => {
    const fetcher: ReviewFetch = async () =>
      new Response(JSON.stringify({ error: { message: "reviews unavailable" } }), { status: 503 })

    await expect(listReviews("workflow-1", fetcher)).rejects.toThrow("reviews unavailable")
  })
})
