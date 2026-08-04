/** @jsxImportSource @opentui/react */

import type { TextareaRenderable } from "@opentui/core"
import { useKeyboard } from "@opentui/react"
import { useRef, useState } from "react"

import { createPlan, type Plan, splitPlanLines } from "./plans"
import { colors, PageIntro, Panel, ShortcutBar } from "./ui"

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

  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageIntro
        kicker="PLAN DRAFT"
        title={`New plan for ${projectName}`}
        description="Move through the fields with Tab. Keep the requirement concrete; one acceptance criterion per line."
        meta={`field ${fieldIndex + 1} / ${editorFields.length}`}
      />

      <Panel
        title="PLAN CONTENT"
        borderColor={colors.accent}
        backgroundColor={colors.surfaceAccent}
      >
        <text fg={field === "title" ? colors.accent : colors.dim}>1 Title</text>
        <input
          value={title}
          placeholder="Example: Add Markdown blog"
          focused={field === "title"}
          onInput={setTitle}
          onSubmit={() => setField("requirement")}
        />

        <text fg={field === "requirement" ? colors.accent : colors.dim}>2 Requirement</text>
        <textarea
          ref={requirementRef}
          width="100%"
          height={6}
          initialValue={requirement}
          placeholder="Describe the feature, expected behavior, and important context..."
          focused={field === "requirement"}
          wrapMode="word"
          backgroundColor={colors.surface}
          focusedBackgroundColor={colors.surfaceRaised}
          onContentChange={() => setRequirement(requirementRef.current?.plainText ?? "")}
        />

        <box flexDirection="row" gap={2} flexGrow={1}>
          <box width="50%" flexDirection="column" gap={1}>
            <text fg={field === "criteria" ? colors.accent : colors.dim}>
              3 Acceptance criteria — one per line
            </text>
            <textarea
              ref={criteriaRef}
              width="100%"
              height={5}
              initialValue={criteria}
              placeholder={"User can list articles\nUser can open article details"}
              focused={field === "criteria"}
              wrapMode="word"
              backgroundColor={colors.surface}
              focusedBackgroundColor={colors.surfaceRaised}
              onContentChange={() => setCriteria(criteriaRef.current?.plainText ?? "")}
            />
          </box>

          <box width="50%" flexDirection="column" gap={1}>
            <text fg={field === "constraints" ? colors.accent : colors.dim}>
              4 Constraints — one per line
            </text>
            <textarea
              ref={constraintsRef}
              width="100%"
              height={5}
              initialValue={constraints}
              placeholder={"Use the existing stack\nDo not add an external database"}
              focused={field === "constraints"}
              wrapMode="word"
              backgroundColor={colors.surface}
              focusedBackgroundColor={colors.surfaceRaised}
              onContentChange={() => setConstraints(constraintsRef.current?.plainText ?? "")}
            />
          </box>
        </box>
      </Panel>

      <ShortcutBar
        shortcuts={[
          { key: "Tab", label: "next field" },
          { key: "Shift+Tab", label: "previous" },
          { key: "Ctrl+S", label: "save draft" },
          { key: "Esc", label: "cancel" },
        ]}
      />
      {message ? <text fg={busy ? colors.warning : colors.danger}>{message}</text> : null}
    </box>
  )
}
