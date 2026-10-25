import { useState, useRef } from "react"
import { TextAttributes, type ScrollBoxRenderable } from "@opentui/core"
import { useKeyboard, useTerminalDimensions } from "@opentui/react"
import { useTheme } from "./context/theme"
import { useDialog } from "./ui/dialog"
import type { Project } from "../api/generated"
import { formatRelative } from "../format"
import { formatPriority, statusTone, type Lookups, type Store } from "./store"
import { useTone } from "./table"
import { PRIORITY_TONE, PROJECT_STATUS_TONE } from "./tones"

export function renderProgressBar(done: number, total: number, width = 24): string {
  if (total <= 0) return "—"
  const ratio = Math.max(0, Math.min(1, done / total))
  const filled = Math.round(ratio * width)
  const empty = width - filled
  return `[${"█".repeat(filled)}${"░".repeat(empty)}] ${done}/${total} (${Math.round(ratio * 100)}%)`
}

export function ProjectDetail(props: {
  project: Project
  store: Store
  lookups: Lookups
  now: number
  onOpenIssue: (id: string) => void
  onViewIssues: () => void
}) {
  const theme = useTheme()
  const tone = useTone()
  const dialog = useDialog()
  const dimensions = useTerminalDimensions()
  const scrollRef = useRef<ScrollBoxRenderable | null>(null)
  const [selectedIssueIndex, setSelectedIssueIndex] = useState(0)

  const projectIssues = props.store.issues.filter((issue) => issue.project_id === props.project.id)

  const scrollBy = (delta: number) => {
    const scroll = scrollRef.current
    if (!scroll) return
    if (delta < 0) scroll.stickyScroll = false
    scroll.scrollBy(delta)
  }

  useKeyboard((event) => {
    if (dialog.stack.length) return
    if (event.name === "i") {
      props.onViewIssues()
      event.preventDefault()
      return
    }
    const list = projectIssues
    if (event.name === "down" || event.name === "j") {
      if (list.length > 0) {
        setSelectedIssueIndex((prev) => Math.min(list.length - 1, prev + 1))
      }
      scrollBy(1)
      event.preventDefault()
      return
    }
    if (event.name === "up" || event.name === "k") {
      if (list.length > 0) {
        setSelectedIssueIndex((prev) => Math.max(0, prev - 1))
      }
      scrollBy(-1)
      event.preventDefault()
      return
    }
    if (event.name === "return" || event.name === "enter") {
      const current = list[selectedIssueIndex]
      if (current) {
        props.onOpenIssue(current.id)
        event.preventDefault()
      }
      return
    }
    if (event.name === "pagedown") {
      const scroll = scrollRef.current
      if (scroll) scrollBy(Math.max(1, scroll.viewport.height - 2))
      event.preventDefault()
      return
    }
    if (event.name === "pageup") {
      const scroll = scrollRef.current
      if (scroll) scrollBy(-Math.max(1, scroll.viewport.height - 2))
      event.preventDefault()
    }
  })

  const width = Math.max(40, dimensions.width - 4)

  return (
    <box flexGrow={1} flexShrink={1} minHeight={0} flexDirection="column">
      <scrollbox
        ref={(element: ScrollBoxRenderable) => {
          scrollRef.current = element
        }}
        flexGrow={1}
        flexShrink={1}
        minHeight={0}
        stickyScroll={false}
      >
        <box flexDirection="column" width={width}>
          <box flexDirection="row" alignItems="center" gap={1}>
            <text fg={theme.text.base} attributes={TextAttributes.BOLD}>
              {props.project.icon ?? "📁"} {props.project.title}
            </text>
            <text fg={tone(PROJECT_STATUS_TONE[props.project.status], false)}> [{props.project.status}]</text>
            <text fg={tone(PRIORITY_TONE[props.project.priority], false)}>
              {" "}
              {formatPriority(props.project.priority)}
            </text>
          </box>

          <box flexDirection="row" gap={2} flexWrap="wrap" marginTop={1}>
            <text fg={theme.text.muted}>
              progress:{" "}
              <span
                style={{
                  fg:
                    props.project.done_count === props.project.issue_count && props.project.issue_count > 0
                      ? theme.text.feedback.success.base
                      : theme.text.base,
                }}
              >
                {renderProgressBar(props.project.done_count, props.project.issue_count)}
              </span>
            </text>
            {props.project.lead_type ? (
              <text fg={theme.text.muted}>
                lead:{" "}
                <span style={{ fg: theme.text.base }}>
                  {props.lookups.actorName(props.project.lead_type, props.project.lead_id)}
                </span>
              </text>
            ) : null}
            {props.project.start_date ? (
              <text fg={theme.text.muted}>start: {props.project.start_date}</text>
            ) : null}
            {props.project.due_date ? <text fg={theme.text.muted}>due: {props.project.due_date}</text> : null}
            <text fg={theme.text.muted}>updated {formatRelative(props.project.updated_at, props.now)}</text>
          </box>

          {props.project.description ? (
            <box marginTop={1} flexDirection="column">
              <text fg={theme.text.muted}>description</text>
              <text fg={theme.text.base}>{props.project.description}</text>
            </box>
          ) : null}

          <box marginTop={1} flexDirection="column">
            <box flexDirection="row" justifyContent="space-between" marginBottom={1}>
              <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>
                issues in project ({projectIssues.length})
              </text>
              <text fg={theme.text.muted}>enter open issue  ·  i view in issues tab  ·  esc back</text>
            </box>
            {projectIssues.length > 0 ? (
              projectIssues.map((issue, index) => {
                const isSelected = index === selectedIssueIndex
                return (
                  <box
                    key={issue.id}
                    flexDirection="row"
                    height={1}
                    gap={1}
                    backgroundColor={isSelected ? theme.background.raised.base : undefined}
                    onMouseUp={() => props.onOpenIssue(issue.id)}
                  >
                    <text width={2} fg={isSelected ? theme.text.action.primary.base : theme.text.muted}>
                      {isSelected ? "›" : " "}
                    </text>
                    <text fg={theme.text.muted} width={10}>
                      {issue.identifier}
                    </text>
                    <text fg={tone(statusTone(props.store, issue), false)} width={14} wrapMode="none" truncate>
                      {props.lookups.statusLabel(issue)}
                    </text>
                    <text
                      fg={isSelected ? theme.text.action.primary.base : theme.text.base}
                      flexGrow={1}
                      wrapMode="none"
                      truncate
                    >
                      {issue.title}
                    </text>
                    <text fg={theme.text.muted} width={16} wrapMode="none" truncate>
                      {props.lookups.actorName(issue.assignee_type, issue.assignee_id) || "—"}
                    </text>
                    <text fg={theme.text.muted} width={10}>
                      {formatRelative(issue.last_activity_at ?? issue.updated_at, props.now)}
                    </text>
                  </box>
                )
              })
            ) : (
              <text fg={theme.text.muted}>No issues linked to this project.</text>
            )}
          </box>
        </box>
      </scrollbox>
    </box>
  )
}
