import { useState, useEffect } from "react"
import { TextAttributes } from "@opentui/core"
import { useRenderer, useTerminalDimensions } from "@opentui/react"
import { useKeymapLayer } from "./context/keymap"
import { useTheme } from "./context/theme"
import type { Connection } from "../config"
import { makeDataClient } from "../data"
import { pickIssueResult, writePickResult } from "../pick"
import { Table, type HeaderCell, type Row } from "./table"

export type PickMode = "issue" | "mentions" | "running" | "ask"

type Item = { id: string; row: Row; payload: unknown }

const TITLES: Record<PickMode, string> = {
  issue: "Pick issue to follow",
  mentions: "Pick someone to @",
  running: "Runs on this workspace",
  ask: "Pick who to ask",
}

export function PickIssueApp(props: { connection: Connection; output?: string; mode?: PickMode }) {
  const mode = props.mode ?? "issue"
  const theme = useTheme()
  const renderer = useRenderer()
  const dimensions = useTerminalDimensions()
  const [items, setItems] = useState<Item[]>([])
  const [headers, setHeaders] = useState<HeaderCell[]>([{ text: "ITEM" }])
  const [selectedId, setSelectedId] = useState<string>()
  const [error, setError] = useState<string>()

  const finish = (code: number, payload?: unknown) => {
    if (payload !== undefined) writePickResult(payload, props.output)
    renderer.setTerminalTitle("")
    if (!renderer.isDestroyed) renderer.destroy()
    process.exit(code)
  }

  const submit = () => {
    const item = items.find((entry) => entry.id === selectedId)
    if (!item) return
    finish(0, item.payload)
  }

  const move = (delta: number) => {
    const list = items
    if (list.length === 0) return
    const index = Math.max(0, list.findIndex((row) => row.id === selectedId))
    const next = list[(index + delta + list.length) % list.length]
    if (next) setSelectedId(next.id)
  }

  useEffect(() => {
    const data = makeDataClient(props.connection)
    const load = async () => {
      if (mode === "issue") {
        const page = await data.listIssues({ sort_by: "updated_at", sort_direction: "desc", limit: 200 })
        setHeaders([
          { text: "ID", width: 10 },
          { text: "TITLE" },
          { text: "STATUS", width: 14 },
        ])
        const list = (page.issues ?? []).map((issue) => ({
          id: issue.id,
          payload: { type: "issue", ...pickIssueResult(issue) },
          row: {
            id: issue.id,
            search: `${issue.identifier} ${issue.title}`.toLowerCase(),
            cells: [
              { text: issue.identifier, width: 10, tone: "subdued" as const },
              { text: issue.title },
              { text: issue.status, width: 14 },
            ],
          },
        }))
        setItems(list)
        setSelectedId(list[0]?.id)
        return
      }
      if (mode === "mentions" || mode === "ask") {
        const [members, agents] = await Promise.all([
          data.listMembers(props.connection.workspaceID),
          data.listAgents({ include_archived: true }),
        ])
        setHeaders([
          { text: "KIND", width: 10 },
          { text: "NAME" },
          { text: "DETAIL" },
        ])
        const list: Item[] = [
          ...members.map((member) => ({
            id: `member:${member.user_id}`,
            payload: { type: mode, kind: "member", to: member.name, id: member.user_id },
            row: {
              id: `member:${member.user_id}`,
              search: member.name.toLowerCase(),
              cells: [
                { text: "member", width: 10, tone: "subdued" as const },
                { text: member.name },
                { text: member.role ?? "", tone: "subdued" as const },
              ],
            },
          })),
          ...agents
            .filter((agent) => !agent.archived_at)
            .map((agent) => ({
              id: `agent:${agent.id}`,
              payload: { type: mode, kind: "agent", to: agent.name, id: agent.id },
              row: {
                id: `agent:${agent.id}`,
                search: agent.name.toLowerCase(),
                cells: [
                  { text: "agent", width: 10, tone: "subdued" as const },
                  { text: agent.name },
                  { text: agent.description ?? "", tone: "subdued" as const },
                ],
              },
            })),
        ]
        setItems(list)
        setSelectedId(list[0]?.id)
        return
      }
      const issues = await data.listIssues({ sort_by: "updated_at", sort_direction: "desc", limit: 30 })
      const first = issues.issues?.[0]
      const runs = first ? await data.listIssueTaskRuns(first.identifier ?? first.id) : []
      setHeaders([
        { text: "STATUS", width: 12 },
        { text: "RUN" },
      ])
      const list = (Array.isArray(runs) ? runs : []).map((run: { id?: string; status?: string }, index: number) => {
        const id = run.id ?? String(index)
        return {
          id,
          payload: { type: "running", runId: run.id, status: run.status },
          row: {
            id,
            search: `${run.status ?? ""} ${run.id ?? ""}`.toLowerCase(),
            cells: [
              { text: String(run.status ?? ""), width: 12 },
              { text: String(run.id ?? "") },
            ],
          },
        }
      })
      setItems(list)
      setSelectedId(list[0]?.id)
    }
    void load()
      .catch((caught: unknown) => {
        setError(caught instanceof Error ? caught.message : String(caught))
      })
      .finally(() => void data.dispose())
  }, [props.connection, mode])

  useKeymapLayer(() => ({
    mode: "global",
    commands: [
      { id: "pstack.pick.prev", bind: "up,k", title: "Previous", run: () => move(-1) },
      { id: "pstack.pick.next", bind: "down,j", title: "Next", run: () => move(1) },
      { id: "pstack.pick.submit", bind: "return,enter", title: "Select", run: submit },
      { id: "pstack.pick.cancel", bind: "escape", title: "Cancel", run: () => finish(1) },
    ],
  }))

  return (
    <box flexGrow={1} flexDirection="column" paddingLeft={1} paddingRight={1}>
      <box height={1} flexDirection="row" justifyContent="space-between" flexShrink={0}>
        <text fg={theme.text.base} attributes={TextAttributes.BOLD}>
          {TITLES[mode]}
        </text>
        <text fg={theme.text.muted}>Enter select · Esc cancel</text>
      </box>
      {error ? <text fg={theme.text.feedback.error.base}>{error}</text> : null}
      <Table
        headers={headers}
        rows={items.map((item) => item.row)}
        selectedId={selectedId}
        onSelect={setSelectedId}
        height={Math.max(6, dimensions.height - 3)}
      />
    </box>
  )
}
