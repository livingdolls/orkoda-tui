/** @jsxImportSource @opentui/react */

import type { TextareaRenderable } from "@opentui/core"
import { useKeyboard } from "@opentui/react"
import { useRef, useState } from "react"

import { createPlan, type Plan, splitPlanLines } from "./plans"
import { Banner, BOLD, Card, colors, KeyHints, PageHeader, Section } from "./ui"

type EditorField = "title" | "requirement" | "criteria" | "constraints"

const editorFields: EditorField[] = ["title", "requirement", "criteria", "constraints"]

export function PlanEditor({
  projectID,
  projectName,
  onSaved,
  onCancel,
}: {
  projectID: string
  projectName: string
  onSaved: (plan: Plan) => void
  onCancel: () => void
}) {
  const [field, setField] = useState<EditorField>("title")
  const [title, setTitle] = useState("")
  const [requirement, setRequirement] = useState("")
  const [criteria, setCriteria] = useState("")
  const [constraints, setConstraints] = useState("")
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState("")
  const fieldIndex = editorFields.indexOf(field)

  const requirementRef = useRef<TextareaRenderable>(null)
  const criteriaRef = useRef<TextareaRenderable>(null)
  const constraintsRef = useRef<TextareaRenderable>(null)

  const moveField = (direction: 1 | -1) => {
    const currentIndex = editorFields.indexOf(field)
    const nextIndex = (currentIndex + direction + editorFields.length) % editorFields.length
    setField(editorFields[nextIndex] ?? "title")
  }

  const save = async () => {
    if (busy) {
      return
    }

    const nextRequirement = requirementRef.current?.plainText ?? requirement
    const nextCriteria = criteriaRef.current?.plainText ?? criteria
    const nextConstraints = constraintsRef.current?.plainText ?? constraints
    if (!title.trim() || !nextRequirement.trim()) {
      setMessage("Title and requirement are required.")
      return
    }

    setBusy(true)
    setMessage("Saving plan draft...")
    try {
      const plan = await createPlan(projectID, {
        title: title.trim(),
        requirement: nextRequirement.trim(),
        acceptanceCriteria: splitPlanLines(nextCriteria),
        constraints: splitPlanLines(nextConstraints),
      })
      onSaved(plan)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to save plan")
    } finally {
      setBusy(false)
    }
  }

  useKeyboard((key) => {
    if (key.name === "escape") {
      onCancel()
      return
    }
    if (key.name === "tab") {
      moveField(key.shift ? -1 : 1)
      return
    }
    if (key.ctrl && key.name === "s") {
      void save()
    }
  })

  const fieldLabel = (name: EditorField, label: string) => (
    <text fg={field === name ? colors.accent : colors.faint} attributes={field === name ? BOLD : 0}>
      {label}
    </text>
  )

  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageHeader
        title={`New plan for ${projectName}`}
        description="Describe what you want built. The planning agent turns this into concrete steps. Tab moves between fields."
        meta={`field ${fieldIndex + 1} of ${editorFields.length}`}
      />

      <Section title="Plan content">
        <Card tone="accent">
          {fieldLabel("title", "1 · Title — a short name for this work")}
          <input
            value={title}
            placeholder="Example: Add Markdown blog"
            focused={field === "title"}
            onInput={setTitle}
            onSubmit={() => setField("requirement")}
          />

          {fieldLabel("requirement", "2 · Requirement — what should happen, in your own words")}
          <textarea
            ref={requirementRef}
            width="100%"
            height={6}
            initialValue={requirement}
            placeholder="Describe the feature, expected behavior, and important context..."
            focused={field === "requirement"}
            wrapMode="word"
            backgroundColor={colors.inset}
            focusedBackgroundColor={colors.raised}
            onContentChange={() => setRequirement(requirementRef.current?.plainText ?? "")}
          />

          <box flexDirection="row" gap={2} flexGrow={1}>
            <box width="50%" flexDirection="column" gap={1}>
              {fieldLabel("criteria", "3 · Acceptance criteria — one per line")}
              <textarea
                ref={criteriaRef}
                width="100%"
                height={5}
                initialValue={criteria}
                placeholder={"User can list articles\nUser can open article details"}
                focused={field === "criteria"}
                wrapMode="word"
                backgroundColor={colors.inset}
                focusedBackgroundColor={colors.raised}
                onContentChange={() => setCriteria(criteriaRef.current?.plainText ?? "")}
              />
            </box>

            <box width="50%" flexDirection="column" gap={1}>
              {fieldLabel("constraints", "4 · Constraints — one per line")}
              <textarea
                ref={constraintsRef}
                width="100%"
                height={5}
                initialValue={constraints}
                placeholder={"Use the existing stack\nDo not add an external database"}
                focused={field === "constraints"}
                wrapMode="word"
                backgroundColor={colors.inset}
                focusedBackgroundColor={colors.raised}
                onContentChange={() => setConstraints(constraintsRef.current?.plainText ?? "")}
              />
            </box>
          </box>
        </Card>
      </Section>

      <KeyHints
        shortcuts={[
          { key: "Tab", label: "next field" },
          { key: "Shift+Tab", label: "previous" },
          { key: "Ctrl+S", label: "save draft" },
          { key: "Esc", label: "cancel" },
        ]}
      />
      {message ? (
        <Banner tone={busy ? "warning" : "danger"}>
          <text fg={busy ? colors.warning : colors.danger}>{message}</text>
        </Banner>
      ) : null}
    </box>
  )
}
