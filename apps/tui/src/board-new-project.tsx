/** @jsxImportSource @opentui/react */

import { useKeyboard } from "@opentui/react"
import { useMemo, useState } from "react"

import {
  buildDirectoryPickerItems,
  type DirectoryListing,
  initialDirectory,
  listDirectories,
  visibleDirectoryItems,
} from "./directory-picker"
import { createProject, type Project } from "./projects"
import { Banner, BOLD, Card, Chip, colors, KeyHints, PageHeader } from "./ui"

type Step = "name" | "folder"

export function BoardNewProject({
  onCreated,
  onCancel,
}: {
  onCreated: (project: Project) => void
  onCancel: () => void
}) {
  const [step, setStep] = useState<Step>("name")
  const [name, setName] = useState("")
  const [listing, setListing] = useState<DirectoryListing | null>(null)
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState("")
  const items = useMemo(() => (listing ? buildDirectoryPickerItems(listing) : []), [listing])

  const openDirectory = async (path: string) => {
    if (busy) return
    setBusy(true)
    setMessage("Reading folders...")
    try {
      const next = await listDirectories(path)
      setListing(next)
      setSelectedIndex(0)
      setMessage("")
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to read this folder")
    } finally {
      setBusy(false)
    }
  }

  const goToFolder = () => {
    if (!name.trim()) {
      setMessage("Project name is required.")
      return
    }
    setStep("folder")
    void openDirectory(listing?.currentPath ?? initialDirectory())
  }

  const submit = async (path: string) => {
    if (busy) return
    setBusy(true)
    setMessage("Checking the Git repository...")
    try {
      const project = await createProject(name.trim(), path)
      onCreated(project)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to create project")
    } finally {
      setBusy(false)
    }
  }

  useKeyboard((key) => {
    if (key.name === "escape") {
      if (step === "folder") {
        setStep("name")
        setMessage("")
      } else {
        onCancel()
      }
      return
    }
    if (step !== "folder" || busy || !listing) return

    if (key.name === "down" || key.name === "j") {
      setSelectedIndex((current) => Math.min(current + 1, Math.max(items.length - 1, 0)))
      return
    }
    if (key.name === "up" || key.name === "k") {
      setSelectedIndex((current) => Math.max(current - 1, 0))
      return
    }
    if (key.name === "left" || key.name === "backspace" || key.name === "h") {
      if (listing.parentPath) void openDirectory(listing.parentPath)
      return
    }
    if (key.name === "s") {
      void submit(listing.currentPath)
      return
    }
    if (key.name === "return" || key.name === "enter") {
      const item = items[selectedIndex]
      if (!item) return
      if (item.kind === "select") void submit(item.path)
      else void openDirectory(item.path)
    }
  })

  if (step === "name") {
    return (
      <box flexDirection="column" flexGrow={1} gap={1}>
        <PageHeader
          title="Add a project"
          description="Give the project a friendly name, then choose the folder containing its Git repository."
          meta="step 1 of 2"
        />
        <Card tone="accent">
          <text fg={colors.muted}>A short name is easiest to recognize on the board.</text>
          <input
            value={name}
            placeholder="Example: Orkoda Website"
            focused
            onInput={setName}
            onSubmit={goToFolder}
          />
        </Card>
        {message ? (
          <Banner tone="danger">
            <text fg={colors.danger}>{message}</text>
          </Banner>
        ) : null}
        <KeyHints
          shortcuts={[
            { key: "Enter", label: "choose repository folder" },
            { key: "Esc", label: "cancel" },
          ]}
        />
      </box>
    )
  }

  const visible = visibleDirectoryItems(items, selectedIndex)
  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageHeader
        title="Choose the repository folder"
        description="Move into the folder containing the project's code. Orkoda validates the Git repository before adding it."
        meta={listing?.currentPath ?? "loading"}
      />
      {listing ? (
        <Chip
          label={
            listing.isGitRepository ? "Git repository detected" : "No .git entry in this folder"
          }
          tone={listing.isGitRepository ? "success" : "warning"}
        />
      ) : null}
      <box
        flexDirection="column"
        flexGrow={1}
        padding={1}
        backgroundColor={colors.raised}
        borderStyle="rounded"
        borderColor={colors.line}
      >
        {busy && !listing ? <text fg={colors.warning}>Reading folders...</text> : null}
        {visible.map(({ item, index }) => {
          const selected = index === selectedIndex
          return (
            <box
              key={`${item.kind}:${item.path}`}
              flexDirection="row"
              gap={1}
              paddingLeft={1}
              paddingRight={1}
              backgroundColor={selected ? colors.accentTint : colors.raised}
            >
              <text fg={selected ? colors.accent : colors.faint}>
                {item.kind === "select" ? "◎" : "▸"}
              </text>
              <text fg={selected ? colors.text : colors.muted} attributes={selected ? BOLD : 0}>
                {item.label}
              </text>
              {item.kind === "select" ? <text fg={colors.faint}>use this folder</text> : null}
            </box>
          )
        })}
      </box>
      {message ? (
        <Banner tone={busy ? "warning" : "danger"}>
          <text fg={busy ? colors.warning : colors.danger}>{message}</text>
        </Banner>
      ) : null}
      <KeyHints
        shortcuts={[
          { key: "↑↓", label: "move" },
          { key: "Enter", label: "open / select" },
          { key: "←", label: "up one folder" },
          { key: "S", label: "use current folder" },
          { key: "Esc", label: "back" },
        ]}
      />
    </box>
  )
}
