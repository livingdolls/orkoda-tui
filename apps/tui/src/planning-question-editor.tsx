/** @jsxImportSource @opentui/react */

import type { TextareaRenderable } from "@opentui/core"
import { useKeyboard } from "@opentui/react"
import { useRef, useState } from "react"

import { answerPlanningRun, type PlanningAnswer, type PlanningRun } from "./planning-agent"
import { Banner, BOLD, Card, colors, EmptyState, KeyHints, PageHeader, Section } from "./ui"

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
      <EmptyState
        icon="✓"
        title="No open questions"
        detail="This planning run is already complete."
        shortcut={{ key: "Esc", label: "back to project" }}
      />
    )
  }

  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageHeader
        title="Planning needs your input"
        description="Answer each question, then submit the whole set. The planning agent regenerates the plan with your answers."
        meta={`PLANNING INPUT · ${selectedIndex + 1} of ${openQuestions.length}`}
      />

      <Section title={`Question ${selectedIndex + 1}`}>
        <Card tone="warning">
          <text fg={colors.text} wrapMode="word" attributes={BOLD}>
            {selectedQuestion.question}
          </text>
        </Card>
      </Section>

      <Section title="Your answer">
        <textarea
          key={selectedQuestion.id}
          ref={answerRef}
          width="100%"
          height={8}
          initialValue={answers[selectedQuestion.id] ?? ""}
          placeholder="Provide the decision or missing context..."
          focused
          wrapMode="word"
          backgroundColor={colors.inset}
          focusedBackgroundColor={colors.raised}
          onContentChange={() => {
            const value = answerRef.current?.plainText ?? ""
            setAnswers((current) => ({ ...current, [selectedQuestion.id]: value }))
          }}
        />
      </Section>

      <Section title="Progress">
        <Card>
          {openQuestions.map((question, index) => {
            const answered = (answers[question.id] ?? "").trim() !== ""
            const current = index === selectedIndex
            return (
              <box key={question.id} flexDirection="row" gap={1}>
                <text fg={current ? colors.accent : answered ? colors.success : colors.faint}>
                  {`${current ? "▸" : " "} ${answered ? "✓" : "○"} Question ${index + 1}`}
                </text>
              </box>
            )
          })}
        </Card>
      </Section>

      <KeyHints
        shortcuts={[
          { key: "Tab", label: "next question" },
          { key: "Shift+Tab", label: "previous" },
          { key: "Ctrl+S", label: "submit all" },
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
