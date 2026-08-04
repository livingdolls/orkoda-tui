/** @jsxImportSource @opentui/react */

import { expect, test } from "bun:test"
import { mkdir, mkdtemp, rm } from "node:fs/promises"
import { createServer } from "node:net"
import { tmpdir } from "node:os"
import { dirname, join, relative, resolve } from "node:path"
import { testRender } from "@opentui/react/test-utils"
import { act } from "react"

const runTuiE2E = process.env.ORKODA_TUI_E2E === "1" ? test : test.skip
const repoRoot = resolve(import.meta.dir, "../../..")

type ApiEnvelope<T> = { data: T }
type Project = { id: string; repositories: Array<{ id: string }> }
type Plan = { id: string; status: string }
type WorkflowJob = {
  id: string
  status: string
  version: number
  failure_code?: string
  failure_message?: string
}

function environment(overrides: Record<string, string> = {}): Record<string, string> {
  const result: Record<string, string> = {}
  for (const [key, value] of Object.entries(process.env)) {
    if (value !== undefined) result[key] = value
  }
  Object.assign(result, overrides)
  return result
}

function runCommand(command: string[], cwd: string): string {
  const result = Bun.spawnSync({
    cmd: command,
    cwd,
    env: environment({ GIT_CONFIG_GLOBAL: "/dev/null", GIT_CONFIG_NOSYSTEM: "1" }),
    stdout: "pipe",
    stderr: "pipe",
  })
  const stdout = new TextDecoder().decode(result.stdout)
  const stderr = new TextDecoder().decode(result.stderr)
  if (result.exitCode !== 0) {
    throw new Error(
      `${command.join(" ")} failed with exit code ${result.exitCode}\n${stderr || stdout}`,
    )
  }
  return stdout.trim()
}

async function api<T>(
  baseURL: string,
  token: string,
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const response = await fetch(`${baseURL}${path}`, {
    method,
    headers: {
      accept: "application/json",
      authorization: `Bearer ${token}`,
      ...(body === undefined ? {} : { "content-type": "application/json" }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const payload = await response.text()
  if (!response.ok) {
    throw new Error(`${method} ${path} returned HTTP ${response.status}: ${payload}`)
  }
  if (!payload) return undefined as T
  return (JSON.parse(payload) as ApiEnvelope<T>).data
}

async function unusedTCPPort(): Promise<number> {
  const listener = createServer()
  await new Promise<void>((resolveListen, reject) => {
    listener.once("error", reject)
    listener.listen(0, "127.0.0.1", () => resolveListen())
  })
  const address = listener.address()
  if (!address || typeof address === "string") {
    listener.close()
    throw new Error("could not determine a free TCP port")
  }
  const port = address.port
  await new Promise<void>((resolveClose) => listener.close(() => resolveClose()))
  return port
}

async function waitForHealth(baseURL: string): Promise<void> {
  const deadline = Date.now() + 20_000
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${baseURL}/health/live`)
      if (response.ok) return
    } catch {
      // The daemon is still starting.
    }
    await Bun.sleep(100)
  }
  throw new Error(`daemon did not become healthy at ${baseURL}`)
}

async function waitForJob(baseURL: string, token: string, jobID: string, wanted: string) {
  const deadline = Date.now() + 90_000
  let lastStatus = ""
  while (Date.now() < deadline) {
    const job = await api<WorkflowJob>(baseURL, token, `/api/v1/jobs/${jobID}`)
    lastStatus = job.status
    if (job.status === wanted) return job
    if (["FAILED", "CANCELLED", "REJECTED"].includes(job.status)) {
      throw new Error(
        `workflow reached ${job.status} (${job.failure_code ?? "no-code"}): ${job.failure_message ?? "no message"}`,
      )
    }
    await Bun.sleep(150)
  }
  throw new Error(`workflow ${jobID} did not reach ${wanted} (last status ${lastStatus})`)
}

async function waitForTUIFrame(
  setup: Awaited<ReturnType<typeof testRender>>,
  predicate: (frame: string) => boolean,
  timeoutMs = 15_000,
): Promise<string> {
  const deadline = Date.now() + timeoutMs
  let frame = setup.captureCharFrame()
  while (Date.now() < deadline) {
    frame = setup.captureCharFrame()
    if (predicate(frame)) return frame
    await act(async () => {
      await setup.renderOnce()
      await Bun.sleep(50)
    })
  }
  throw new Error(`TUI frame assertion timed out. Last frame:\n${frame}`)
}

runTuiE2E(
  "reviews and approves a real daemon workflow from the unified kanban board",
  async () => {
    const testRoot = await mkdtemp(join(tmpdir(), "orkoda-board-e2e-"))
    const repositoryRoot = join(testRoot, "repository")
    const stateRoot = join(testRoot, "state")
    const apiBinary = join(stateRoot, "orkoda-api")
    const token = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
    let daemon: ReturnType<typeof Bun.spawn> | undefined

    try {
      await mkdir(repositoryRoot, { recursive: true })
      await mkdir(stateRoot, { recursive: true, mode: 0o700 })
      await Bun.write(
        join(repositoryRoot, "go.mod"),
        "module example.com/orkoda-board-e2e\n\ngo 1.26\n",
      )
      await Bun.write(join(repositoryRoot, "main.go"), "package main\n\nfunc main() {}\n")
      runCommand(["git", "-C", repositoryRoot, "init"], repoRoot)
      runCommand(["git", "-C", repositoryRoot, "checkout", "-b", "main"], repoRoot)
      runCommand(
        ["git", "-C", repositoryRoot, "config", "user.name", "Orkoda Board E2E"],
        repoRoot,
      )
      runCommand(
        ["git", "-C", repositoryRoot, "config", "user.email", "board-e2e@localhost"],
        repoRoot,
      )
      runCommand(["git", "-C", repositoryRoot, "add", "--all"], repoRoot)
      runCommand(
        ["git", "-C", repositoryRoot, "commit", "--no-gpg-sign", "-m", "Initial board fixture"],
        repoRoot,
      )

      const goBinary = process.env.ORKODA_E2E_GO_BIN ?? "/home/dev3/.gvm/gos/go1.26/bin/go"
      const goPath = dirname(goBinary)
      const build = Bun.spawn({
        cmd: [goBinary, "build", "-o", apiBinary, "./cmd/api"],
        cwd: repoRoot,
        env: environment({ PATH: `${goPath}:${process.env.PATH ?? ""}` }),
        stdout: "pipe",
        stderr: "pipe",
      })
      const buildExitCode = await build.exited
      if (buildExitCode !== 0) {
        const buildOutput = await new Response(build.stderr).text()
        throw new Error(`build API daemon failed with exit code ${buildExitCode}\n${buildOutput}`)
      }

      const port = await unusedTCPPort()
      const baseURL = `http://127.0.0.1:${port}`
      daemon = Bun.spawn({
        cmd: [apiBinary],
        cwd: repoRoot,
        env: environment({
          ORKODA_ENV: "test",
          ORKODA_API_HOST: "127.0.0.1",
          ORKODA_API_PORT: String(port),
          ORKODA_DATA_DIR: stateRoot,
          ORKODA_API_TOKEN: token,
          ORKODA_API_TOKEN_FILE: join(stateRoot, "api.token"),
          ORKODA_SANDBOX_MODE: "host",
          ORKODA_ALLOW_UNSANDBOXED_CHECKS: "true",
          ORKODA_WORKSPACE_LEASE_TTL: "30s",
          ORKODA_SHUTDOWN_TIMEOUT: "5s",
          ORKODA_LLM_PROVIDER: "local-fake",
          PATH: `${goPath}:${process.env.PATH ?? ""}`,
        }),
        stdout: "ignore",
        stderr: "ignore",
      })
      await waitForHealth(baseURL)

      const project = await api<Project>(baseURL, token, "/api/v1/projects", "POST", {
        name: "E2E board project",
        repository_path: repositoryRoot,
      })
      const plan = await api<Plan>(baseURL, token, `/api/v1/projects/${project.id}/plans`, "POST", {
        title: "Kanban approval flow",
        requirement: "Exercise approval from the unified board.",
        acceptance_criteria: ["Approval from the Board is persisted by the daemon."],
        constraints: [],
      })
      await api<Plan>(baseURL, token, `/api/v1/plans/${plan.id}`, "PATCH", {
        title: "Kanban approval flow",
        status: "READY",
      })
      const job = await api<WorkflowJob>(
        baseURL,
        token,
        `/api/v1/projects/${project.id}/jobs`,
        "POST",
        {
          plan_id: plan.id,
          repository_id: project.repositories[0]?.id,
          base_branch: "main",
        },
      )
      await api<WorkflowJob>(baseURL, token, `/api/v1/jobs/${job.id}/start`, "POST", {
        expected_version: job.version,
        details: { source: "board-e2e" },
      })
      await waitForJob(baseURL, token, job.id, "WAITING_FOR_APPROVAL")

      process.env.ORKODA_DAEMON_URL = baseURL
      delete process.env.ORKODA_API_TOKEN
      delete process.env.ORKODA_API_TOKEN_FILE
      process.env.ORKODA_DATA_DIR = relative(repoRoot, stateRoot)
      const { App } = await import("./app")
      const setup = await testRender(<App />, {
        width: 160,
        height: 52,
        exitOnCtrlC: false,
        screenMode: "main-screen",
        consoleMode: "disabled",
      })

      try {
        const boardFrame = await waitForTUIFrame(
          setup,
          (frame) =>
            frame.includes("Board") &&
            frame.includes("Needs You") &&
            frame.includes("Kanban approval flow") &&
            frame.includes("Ready for your review"),
        )
        expect(boardFrame).toContain("E2E board project")

        await act(async () => {
          await setup.mockInput.pressKey("enter")
        })
        const detailFrame = await waitForTUIFrame(
          setup,
          (frame) =>
            frame.includes("Kanban approval flow") &&
            frame.includes("Automated checks") &&
            frame.includes("AI review") &&
            frame.includes("Changed files and diff"),
        )
        expect(detailFrame).toContain("Ready for your review")

        await act(async () => {
          await setup.mockInput.pressKey("a")
        })
        const decisionFrame = await waitForTUIFrame(
          setup,
          (frame) => frame.includes("Approve this result") && frame.includes("Verify the snapshot"),
        )
        expect(decisionFrame).toContain("Approval note")
        expect(setup.renderer.currentFocusedEditor?.focused).toBe(true)

        await act(async () => {
          await setup.mockInput.typeText("Approved from unified Board")
        })
        await waitForTUIFrame(setup, (frame) => frame.includes("Approved from unified Board"))

        await act(async () => {
          await setup.mockInput.pressKey("s", { ctrl: true })
        })
        const approvedFrame = await waitForTUIFrame(
          setup,
          (frame) => frame.includes("Decision applied") && frame.includes("Approved"),
        )
        expect(approvedFrame).toContain("workflow v")

        await act(async () => {
          setup.resize(80, 30)
          await setup.renderOnce()
        })
        const compactFrame = await waitForTUIFrame(
          setup,
          (frame) => frame.includes("ORKODA") && frame.includes("Approved"),
        )
        expect(compactFrame).toContain("Kanban approval flow")
      } finally {
        await act(async () => {
          await setup.flush({ maxPasses: 10 })
        })
        act(() => setup.renderer.destroy())
      }

      const persistedJob = await api<WorkflowJob>(baseURL, token, `/api/v1/jobs/${job.id}`)
      expect(persistedJob.status).toBe("APPROVED")
    } finally {
      if (daemon) {
        daemon.kill("SIGINT")
        const result = await Promise.race([
          daemon.exited.then(() => "stopped" as const),
          Bun.sleep(8_000).then(() => "timed-out" as const),
        ])
        if (result === "timed-out") {
          daemon.kill()
          await daemon.exited
        }
      }
      await rm(testRoot, { recursive: true, force: true })
    }
  },
  { timeout: 120_000 },
)
