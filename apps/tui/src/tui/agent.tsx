import { useState, useEffect, useRef } from "react"
import { TextAttributes, type ScrollBoxRenderable } from "@opentui/core"
import { useKeyboard, useTerminalDimensions } from "@opentui/react"
import { useTheme } from "./context/theme"
import { useDialog } from "./ui/dialog"
import type { Agent, AgentTask, SkillSummary } from "../api/generated"
import type { DataClient } from "../data"
import type { RealtimeClient } from "../realtime"
import { formatRelative, formatTokens, shortId } from "../format"
import { ItemList, type ItemListRow } from "./item-list"
import { MarkdownView } from "./markdown-view"
import type { Lookups, Store } from "./store"
import { useTone } from "./table"
import { AGENT_STATUS_TONE, TASK_STATUS_TONE } from "./tones"

type Detail = {
  agent: Agent
  tasks: readonly AgentTask[]
  skills: readonly SkillSummary[]
}

export function AgentDetail(props: {
  id: string
  data: DataClient
  store: Store
  lookups: Lookups
  height: number
  now: number
  realtime?: RealtimeClient
}) {
  const theme = useTheme()
  const tone = useTone()
  const dialog = useDialog()
  const dimensions = useTerminalDimensions()
  const [detail, setDetail] = useState<Detail>()
  const [error, setError] = useState<string>()
  const scrollRef = useRef<ScrollBoxRenderable | null>(null)

  const load = async () => {
    const [agent, tasks, skills] = await Promise.all([
      props.data.getAgent(props.id),
      props.data.listAgentTasks(props.id),
      props.data.listAgentSkills(props.id),
    ])
    setDetail({ agent, tasks, skills })
    setError(undefined)
  }

  useEffect(() => {
    setDetail(undefined)
    setError(undefined)
    void load().catch((reason) => setError(reason instanceof Error ? reason.message : String(reason)))
  }, [props.id])

  useEffect(() => {
    const realtime = props.realtime
    if (!realtime) {
      const timer = setInterval(
        () => void load().catch((reason) => setError(reason instanceof Error ? reason.message : String(reason))),
        10_000,
      )
      return () => clearInterval(timer)
    }
    const off = realtime.onWorkspace(() => {
      void load().catch((reason) => setError(reason instanceof Error ? reason.message : String(reason)))
    })
    return off
  }, [props.realtime, props.id])

  const scrollBy = (delta: number) => {
    const scroll = scrollRef.current
    if (!scroll) return
    if (delta < 0) scroll.stickyScroll = false
    scroll.scrollBy(delta)
  }

  useKeyboard((event) => {
    if (dialog.stack.length) return
    const scroll = scrollRef.current
    if (!scroll) return
    const page = Math.max(1, scroll.viewport.height - 1)
    if (event.name === "j" || event.name === "down") {
      scrollBy(1)
      event.preventDefault()
      return
    }
    if (event.name === "k" || event.name === "up") {
      scrollBy(-1)
      event.preventDefault()
      return
    }
    if (event.name === "pagedown") {
      scrollBy(page)
      event.preventDefault()
      return
    }
    if (event.name === "pageup") {
      scrollBy(-page)
      event.preventDefault()
    }
  })

  const width = Math.max(40, dimensions.width - 4)

  const taskRows = (tasks: readonly AgentTask[]): ItemListRow[] =>
    tasks.map((task) => ({
      id: task.id,
      cells: [
        { text: task.status, width: 10, tone: TASK_STATUS_TONE[task.status] },
        { text: shortId(task.id), width: 14 },
        {
          text: [
            formatRelative(task.created_at, props.now),
            task.issue_id ? `issue ${shortId(task.issue_id)}` : "",
            task.kind ?? "",
          ]
            .filter(Boolean)
            .join(" · "),
        },
      ],
      lines: [
        ...(task.trigger_summary ?? task.handoff_note ? [task.trigger_summary ?? task.handoff_note!] : []),
        ...(task.error ? [task.error] : []),
        ...(task.usage && task.usage.length > 0
          ? task.usage.map(
              (usage) =>
                `${usage.model}: ${formatTokens(usage.input_tokens)} in / ${formatTokens(usage.output_tokens)} out`,
            )
          : []),
      ],
    }))

  const skillRows = (skills: readonly SkillSummary[]): ItemListRow[] =>
    skills.map((skill) => ({
      id: skill.id,
      cells: [
        {
          text: skill.enabled === false ? `${skill.name} (off)` : skill.name,
          width: 28,
        },
        { text: skill.description ?? "", tone: "subdued" },
      ],
    }))

  if (!detail) {
    return (
      <box flexGrow={1} flexShrink={1} minHeight={0} flexDirection="column">
        <box paddingTop={1}>
          <text fg={error ? theme.text.feedback.error.base : theme.text.muted}>
            {error ?? "Loading agent…"}
          </text>
        </box>
      </box>
    )
  }

  const { agent, tasks, skills } = detail

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
        <box flexDirection="column" width={width} gap={1}>
          <box flexDirection="row">
            <text fg={theme.text.base} attributes={TextAttributes.BOLD}>
              {agent.name}
            </text>
            <text fg={tone(AGENT_STATUS_TONE[agent.status], false)}>
              {"  "}
              {agent.status}
            </text>
            {agent.archived_at ? <text fg={theme.text.feedback.warning.base}>  archived</text> : null}
          </box>
          <box flexDirection="row" gap={2} flexWrap="wrap">
            <text fg={theme.text.muted}>model {agent.model || "·"}</text>
            {agent.thinking_level ? <text fg={theme.text.muted}>effort {agent.thinking_level}</text> : null}
            <text fg={theme.text.muted}>runtime {props.lookups.runtimeName(agent.runtime_id)}</text>
            <text fg={theme.text.muted}>{agent.runtime_mode}</text>
            <text fg={theme.text.muted}>visibility {agent.visibility}</text>
            <text fg={theme.text.muted}>max {agent.max_concurrent_tasks} tasks</text>
            {agent.has_custom_env ? (
              <text fg={theme.text.muted}>env {agent.custom_env_key_count ?? 0} keys</text>
            ) : null}
            {agent.mcp_config_redacted ? <text fg={theme.text.muted}>mcp configured (hidden)</text> : null}
          </box>

          {agent.description ? (
            <box flexDirection="column" gap={0}>
              <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>
                description
              </text>
              <MarkdownView source={agent.description} width={width} />
            </box>
          ) : null}

          {agent.instructions ? (
            <box flexDirection="column" gap={0}>
              <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>
                instructions
              </text>
              <MarkdownView source={agent.instructions} width={width} />
            </box>
          ) : null}

          {(agent.custom_args ?? []).length > 0 ? (
            <box flexDirection="column" gap={0}>
              <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>
                custom args
              </text>
              <text fg={theme.text.base}>{(agent.custom_args ?? []).join(" ")}</text>
            </box>
          ) : null}

          <ItemList title="skills" rows={skillRows(skills)} empty="No skills assigned." />

          <ItemList title="recent tasks" rows={taskRows(tasks)} empty="No tasks yet." />
        </box>
      </scrollbox>
    </box>
  )
}
