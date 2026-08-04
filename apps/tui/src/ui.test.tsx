/** @jsxImportSource @opentui/react */

import { expect, test } from "bun:test"
import { testRender } from "@opentui/react/test-utils"
import { act } from "react"

import { CommandPalette } from "./ui"

test("command palette renders searchable commands and focus affordances", async () => {
  const setup = await testRender(
    <CommandPalette
      commands={[
        { id: "projects", label: "Open Projects", detail: "Repository registry", shortcut: "1" },
        { id: "diagnostics", label: "Open Diagnostics", detail: "Daemon health", shortcut: "5" },
      ]}
      onSelect={() => undefined}
      onClose={() => undefined}
    />,
    {
      width: 100,
      height: 30,
      exitOnCtrlC: false,
      screenMode: "main-screen",
      consoleMode: "disabled",
    },
  )

  try {
    await act(async () => {
      await setup.renderOnce()
    })
    const frame = setup.captureCharFrame()
    expect(frame).toContain("SEARCH")
    expect(frame).toContain("Open Projects")
    expect(frame).toContain("↑↓ select")
  } finally {
    act(() => {
      setup.renderer.destroy()
    })
  }
})
