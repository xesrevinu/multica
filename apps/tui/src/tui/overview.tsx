import { useMemo } from "react"
import { TextAttributes } from "@opentui/core"
import { useTheme } from "./context/theme"
import type { IssueStatusCategory } from "../api/generated"
import { formatRelative } from "../format"
import { renderProgressBar } from "./project"
import { machineSummaries, type Store } from "./store"
import { useTone } from "./table"
import { CATEGORY_TONE as TONES } from "./tones"

const CATEGORIES: { key: IssueStatusCategory; label: string }[] = [
  { key: "in_progress", label: "in progress" },
  { key: "in_review", label: "in review" },
  { key: "todo", label: "todo" },
  { key: "backlog", label: "backlog" },
  { key: "done", label: "done" },
  { key: "blocked", label: "blocked" },
  { key: "cancelled", label: "cancelled" },
]

export function Overview(props: { store: Store; height: number; now: number }) {
  const theme = useTheme()
  const tone = useTone()

  const byCategory = useMemo(() => {
    const counts = new Map<IssueStatusCategory, number>()
    for (const issue of props.store.issues) {
      const category =
        issue.status_category ?? props.store.statuses.find((status) => status.key === issue.status)?.category
      if (!category) continue
      counts.set(category, (counts.get(category) ?? 0) + 1)
    }
    return counts
  }, [props.store.issues, props.store.statuses])

  const agentStats = useMemo(() => {
    const stats = { working: 0, idle: 0, offline: 0, error: 0, blocked: 0 }
    for (const agent of props.store.agents) {
      if (agent.archived_at) continue
      if (agent.status in stats) stats[agent.status as keyof typeof stats]++
    }
    return stats
  }, [props.store.agents])

  const machines = useMemo(() => machineSummaries(props.store.runtimes), [props.store.runtimes])

  return (
    <box flexGrow={1} flexShrink={1} minHeight={0} flexDirection="column" gap={1}>
      {/* Workspace Header Card */}
      <box
        flexDirection="row"
        alignItems="center"
        justifyContent="space-between"
        backgroundColor={theme.background.raised.base}
        paddingLeft={1}
        paddingRight={1}
        height={1}
      >
        <box flexDirection="row" gap={1}>
          <text fg={theme.text.action.primary.base} attributes={TextAttributes.BOLD}>
            {props.store.workspace?.name ?? "Loading…"}
          </text>
          <text fg={theme.text.muted}>
            prefix {props.store.workspace?.issue_prefix ?? "—"} · {props.store.members.length} members
          </text>
        </box>
        <text fg={theme.text.muted}>
          {props.store.me ? `${props.store.me.name} <${props.store.me.email}>` : ""}
        </text>
      </box>

      {/* Issues Pipeline Section */}
      <box flexDirection="column">
        <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>
          issues pipeline ({props.store.issuesTotal || props.store.issues.length})
        </text>
        <box flexDirection="row" gap={2} flexWrap="wrap" marginTop={0}>
          {CATEGORIES.map((category) => (
            <text key={category.key} fg={tone(TONES[category.key], false)}>
              {category.label}{" "}
              <span style={{ fg: theme.text.base }}>{byCategory.get(category.key) ?? 0}</span>
            </text>
          ))}
        </box>
      </box>

      {/* Projects Progress Section */}
      {props.store.projects.length > 0 ? (
        <box flexDirection="column">
          <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>
            projects ({props.store.projects.length})
          </text>
          {props.store.projects.slice(0, 4).map((project) => (
            <box key={project.id} flexDirection="row" height={1} gap={1} alignItems="center">
              <text fg={theme.text.base} width={20} wrapMode="none" truncate>
                {project.icon ?? "📁"} {project.title}
              </text>
              <text fg={theme.text.muted} width={34} wrapMode="none">
                {renderProgressBar(project.done_count, project.issue_count, 12)}
              </text>
              <text fg={theme.text.muted}>[{project.status}]</text>
            </box>
          ))}
        </box>
      ) : null}

      {/* Agents & Machines Section */}
      <box flexDirection="column">
        <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>
          agents & machines ({props.store.agents.length} agents · {props.store.runtimes.length} runtimes)
        </text>
        <box flexDirection="row" gap={2} marginTop={0}>
          <text fg={theme.text.feedback.success.base}>● {agentStats.working} working</text>
          {props.store.orchIssues.filter((i) => i.attention.bucket === "needs_you").length > 0 ? (
            <text fg={theme.text.feedback.warning.base}>
              ▲ {props.store.orchIssues.filter((i) => i.attention.bucket === "needs_you").length} needs you
            </text>
          ) : null}
          <text fg={theme.text.base}>○ {agentStats.idle} idle</text>
          {agentStats.blocked > 0 ? (
            <text fg={theme.text.feedback.warning.base}>▲ {agentStats.blocked} blocked</text>
          ) : null}
          {agentStats.error > 0 ? (
            <text fg={theme.text.feedback.error.base}>✖ {agentStats.error} error</text>
          ) : null}
          <text fg={theme.text.muted}>
            machines:{" "}
            {machines.map((m, idx) => (
              <span key={m.name} style={{ fg: m.isOnline ? theme.text.feedback.success.base : theme.text.muted }}>
                {m.isOnline ? "●" : "○"} {m.name} ({m.onlineRuntimes}/{m.totalRuntimes})
                {idx < machines.length - 1 ? "  " : ""}
              </span>
            ))}
          </text>
        </box>
      </box>

      {/* Active Tasks / Working Now */}
      <box flexDirection="column" flexShrink={1} minHeight={0}>
        <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>
          active tasks
        </text>
        {props.store.working.length > 0 ? (
          props.store.working.map((agent) => (
            <box key={agent.id} flexDirection="row" height={1} gap={1}>
              <text fg={theme.text.feedback.success.base}>●</text>
              <text fg={theme.text.base} width={24} wrapMode="none" truncate>
                {agent.name}
              </text>
              <text fg={theme.text.muted}>
                {agent.running_task_count} task{agent.running_task_count === 1 ? "" : "s"}
                {(agent.issue_ids ?? []).length > 0 ? ` on ${(agent.issue_ids ?? []).length} issue(s)` : ""}
              </text>
            </box>
          ))
        ) : (
          <text fg={theme.text.muted}>No active agent tasks right now.</text>
        )}
      </box>

      {/* Recent Runtimes */}
      <box flexDirection="column">
        <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>
          runtimes
        </text>
        {props.store.runtimes.slice(0, 6).map((runtime) => (
          <box key={runtime.id} flexDirection="row" height={1} gap={1}>
            <text fg={runtime.status === "online" ? theme.text.feedback.success.base : theme.text.muted}>
              {runtime.status === "online" ? "●" : "○"}
            </text>
            <text fg={theme.text.base} width={26} wrapMode="none" truncate>
              {runtime.custom_name ?? runtime.name}
            </text>
            <text fg={theme.text.muted} width={12} wrapMode="none" truncate>
              {runtime.provider}
            </text>
            <text fg={theme.text.muted}>{formatRelative(runtime.last_seen_at, props.now)}</text>
          </box>
        ))}
      </box>
    </box>
  )
}
