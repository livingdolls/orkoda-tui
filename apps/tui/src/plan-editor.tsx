/** @jsxImportSource @opentui/react */

import type { TextareaRenderable } from "@opentui/core"
import { useKeyboard } from "@opentui/react"
import { useRef, useState } from "react"

import { createPlan, type Plan, splitPlanLines } from "./plans"

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
      <box flexDirection="row" justifyContent="space-between">
        <text fg="#E2E8F0">New plan for {projectName}</text>
        <text fg="#64748B">DRAFT</text>
      </box>

      <text fg={field === "title" ? "#7DD3FC" : "#64748B"}>Title</text>
      <input
        value={title}
        placeholder="Example: Add Markdown blog"
        focused={field === "title"}
        onInput={setTitle}
        onSubmit={() => setField("requirement")}
      />

      <text fg={field === "requirement" ? "#7DD3FC" : "#64748B"}>Requirement</text>
      <textarea
        ref={requirementRef}
        height={6}
        initialValue={requirement}
        placeholder="Describe the feature, expected behavior, and important context..."
        focused={field === "requirement"}
        wrapMode="word"
        borderStyle="rounded"
        borderColor={field === "requirement" ? "#7DD3FC" : "#334155"}
        onContentChange={() => setRequirement(requirementRef.current?.plainText ?? "")}
      />

      <box flexDirection="row" gap={2} flexGrow={1}>
        <box width="50%" flexDirection="column" gap={1}>
          <text fg={field === "criteria" ? "#7DD3FC" : "#64748B"}>
            Acceptance criteria — one per line
          </text>
          <textarea
            ref={criteriaRef}
            height={5}
            initialValue={criteria}
            placeholder={"User can list articles\nUser can open article details"}
            focused={field === "criteria"}
            wrapMode="word"
            borderStyle="rounded"
            borderColor={field === "criteria" ? "#7DD3FC" : "#334155"}
            onContentChange={() => setCriteria(criteriaRef.current?.plainText ?? "")}
          />
        </box>

        <box width="50%" flexDirection="column" gap={1}>
          <text fg={field === "constraints" ? "#7DD3FC" : "#64748B"}>
            Constraints — one per line
          </text>
          <textarea
            ref={constraintsRef}
            height={5}
            initialValue={constraints}
            placeholder={"Use the existing stack\nDo not add an external database"}
            focused={field === "constraints"}
            wrapMode="word"
            borderStyle="rounded"
            borderColor={field === "constraints" ? "#7DD3FC" : "#334155"}
            onContentChange={() => setConstraints(constraintsRef.current?.plainText ?? "")}
          />
        </box>
      </box>

      <text fg="#64748B">Tab/Shift+Tab switch field • Ctrl+S save draft • Esc cancel</text>
      {message ? <text fg={busy ? "#FACC15" : "#F87171"}>{message}</text> : null}
    </box>
  )
}
