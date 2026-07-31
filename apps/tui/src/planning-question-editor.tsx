/** @jsxImportSource @opentui/react */

import type { TextareaRenderable } from "@opentui/core"
import { useKeyboard } from "@opentui/react"
import { useRef, useState } from "react"

import {
  answerPlanningRun,
  type PlanningAnswer,
  type PlanningRun,
} from "./planning-agent"

export function PlanningQuestionEditor({
  run,
  onSubmitted,
  onCancel,
}: {
  run: PlanningRun
  onSubmitted: (run: PlanningRun) => void
  onCancel: () => void
}) {
  const openQuestions = run.questions.filter((question) => question.status === "OPEN")
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [answers, setAnswers] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState("")
  const answerRef = useRef<TextareaRenderable>(null)

  const selectedQuestion = openQuestions[selectedIndex] ?? null
  const captureAnswer = () => {
    if (!selectedQuestion) {
      return
    }
    const value = answerRef.current?.plainText ?? answers[selectedQuestion.id] ?? ""
    setAnswers((current) => ({ ...current, [selectedQuestion.id]: value }))
  }

  const moveQuestion = (direction: 1 | -1) => {
    captureAnswer()
    setSelectedIndex((current) => {
      const next = current + direction
      return Math.min(Math.max(next, 0), Math.max(openQuestions.length - 1, 0))
    })
    setMessage("")
  }

  const submit = async () => {
    if (busy || !selectedQuestion) {
      return
    }
    const currentValue = answerRef.current?.plainText ?? answers[selectedQuestion.id] ?? ""
    const nextAnswers = { ...answers, [selectedQuestion.id]: currentValue }
    const payload: PlanningAnswer[] = openQuestions.map((question) => ({
      questionID: question.id,
      answer: (nextAnswers[question.id] ?? "").trim(),
    }))
    const missingIndex = payload.findIndex((answer) => answer.answer === "")
    if (missingIndex >= 0) {
      setAnswers(nextAnswers)
      setSelectedIndex(missingIndex)
      setMessage("Every open question requires an answer.")
      return
    }

    setBusy(true)
    setMessage("Submitting answers and regenerating the plan...")
    try {
      const nextRun = await answerPlanningRun(run.id, payload)
      onSubmitted(nextRun)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to submit planning answers")
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
      moveQuestion(key.shift ? -1 : 1)
      return
    }
    if (key.ctrl && key.name === "s") {
      void submit()
    }
  })

  if (!selectedQuestion) {
    return (
      <box flexDirection="column" gap={1}>
        <text fg="#FACC15">This planning run has no open questions.</text>
        <text fg="#64748B">Esc returns to the project.</text>
      </box>
    )
  }

  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <box flexDirection="row" justifyContent="space-between">
        <text fg="#E2E8F0">Planning needs your input</text>
        <text fg="#FACC15">
          {selectedIndex + 1}/{openQuestions.length}
        </text>
      </box>

      <box borderStyle="rounded" borderColor="#334155" padding={1} flexDirection="column">
        <text fg="#7DD3FC">{selectedQuestion.question}</text>
      </box>

      <text fg="#64748B">Your answer</text>
      <textarea
        key={selectedQuestion.id}
        ref={answerRef}
        width="100%"
        height={8}
        initialValue={answers[selectedQuestion.id] ?? ""}
        placeholder="Provide the decision or missing context..."
        focused
        wrapMode="word"
        backgroundColor="#11182B"
        focusedBackgroundColor="#172036"
        onContentChange={() => {
          const value = answerRef.current?.plainText ?? ""
          setAnswers((current) => ({ ...current, [selectedQuestion.id]: value }))
        }}
      />

      <box flexDirection="column" marginTop={1}>
        {openQuestions.map((question, index) => {
          const answered = (answers[question.id] ?? "").trim() !== ""
          return (
            <text key={question.id} fg={index === selectedIndex ? "#7DD3FC" : "#64748B"}>
              {`${index === selectedIndex ? "›" : " "} ${answered ? "✓" : "○"} Question ${index + 1}`}
            </text>
          )
        })}
      </box>

      <text fg="#64748B">Tab/Shift+Tab switch question • Ctrl+S submit all • Esc cancel</text>
      {message ? <text fg={busy ? "#FACC15" : "#F87171"}>{message}</text> : null}
    </box>
  )
}
