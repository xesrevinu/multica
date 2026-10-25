import { useState, useEffect } from "react"
import { TextAttributes } from "@opentui/core"
import { useKeyboard } from "@opentui/react"
import { useTheme } from "./context/theme"
import { useDialog } from "./ui/dialog"
import type { Autopilot, AutopilotRun, AutopilotTrigger } from "../api/generated"
import type { DataClient } from "../data"
import { formatRelative } from "../format"
import type { Lookups } from "./store"

export function AutopilotDetail(props: {
  id: string
  data: DataClient
  lookups: Lookups
  now: number
}) {
  const theme = useTheme()
  const dialog = useDialog()
  const [autopilot, setAutopilot] = useState<Autopilot>()
  const [triggers, setTriggers] = useState<readonly AutopilotTrigger[]>([])
  const [runs, setRuns] = useState<readonly AutopilotRun[]>([])
  const [error, setError] = useState<string>()

  const load = async () => {
    const [detail, runList] = await Promise.all([
      props.data.getAutopilot(props.id),
      props.data.listAutopilotRuns(props.id, { limit: 12, offset: 0 }),
    ])
    setAutopilot(detail.autopilot)
    setTriggers(detail.triggers)
    setRuns(runList.runs)
    setError(undefined)
  }

  useEffect(() => {
    setAutopilot(undefined)
    setError(undefined)
    void load().catch((reason) => setError(reason instanceof Error ? reason.message : String(reason)))
  }, [props.id])

  useKeyboard((event) => {
    if (dialog.stack.length) return
    if (event.name === "t" && autopilot?.can_write !== false) {
      void props.data
        .triggerAutopilot(props.id)
        .then(load)
        .catch((reason) => setError(reason instanceof Error ? reason.message : String(reason)))
      event.preventDefault()
    }
  })

  if (!autopilot) {
    return (
      <box flexGrow={1} flexShrink={1} minHeight={0} flexDirection="column" gap={1}>
        <text fg={error ? theme.text.feedback.error.base : theme.text.muted}>
          {error ?? "Loading autopilot…"}
        </text>
      </box>
    )
  }

  return (
    <box flexGrow={1} flexShrink={1} minHeight={0} flexDirection="column" gap={1}>
      <box flexDirection="column" gap={1}>
        <text fg={theme.text.base} attributes={TextAttributes.BOLD}>
          {autopilot.title}
        </text>
        <text fg={theme.text.muted}>
          {autopilot.status}
          {" · "}
          {autopilot.execution_mode.replaceAll("_", " ")}
          {" · "}
          {props.lookups.actorName(autopilot.assignee_type ?? "agent", autopilot.assignee_id) || "—"}
          {autopilot.last_run_at ? ` · last ${formatRelative(autopilot.last_run_at, props.now)}` : ""}
        </text>
        {autopilot.description ? (
          <text fg={theme.text.base} wrapMode="word">
            {autopilot.description}
          </text>
        ) : null}
        {error ? <text fg={theme.text.feedback.error.base}>{error}</text> : null}
        <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>
          triggers
        </text>
        {triggers.length > 0 ? (
          triggers.map((trigger) => (
            <text key={trigger.id} fg={theme.text.base}>
              {trigger.enabled ? "●" : "○"} {trigger.kind}
              {trigger.cron_expression ? ` ${trigger.cron_expression}` : ""}
              {trigger.label ? ` · ${trigger.label}` : ""}
              {trigger.next_run_at ? ` · next ${formatRelative(trigger.next_run_at, props.now)}` : ""}
            </text>
          ))
        ) : (
          <text fg={theme.text.muted}>No triggers</text>
        )}
        <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>
          recent runs
        </text>
        {runs.length > 0 ? (
          runs.map((run) => (
            <text key={run.id} fg={theme.text.base}>
              {run.status} · {run.source} · {formatRelative(run.triggered_at, props.now)}
              {run.failure_reason ? ` · ${run.failure_reason}` : ""}
            </text>
          ))
        ) : (
          <text fg={theme.text.muted}>No runs yet</text>
        )}
        <text fg={theme.text.muted}>t run now</text>
      </box>
    </box>
  )
}
