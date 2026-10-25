import { useMemo } from "react"
import { TextAttributes } from "@opentui/core"
import { useTheme } from "./context/theme"
import type { DashboardUsageByAgent } from "../api/generated"
import { formatTokens } from "../format"
import type { Lookups } from "./store"

export function UsageView(props: {
  rows: readonly DashboardUsageByAgent[]
  lookups: Lookups
  error?: string
}) {
  const theme = useTheme()
  const totals = useMemo(
    () =>
      props.rows.reduce(
        (sum, row) => ({
          input: sum.input + row.input_tokens,
          output: sum.output + row.output_tokens,
          tasks: sum.tasks + row.task_count,
        }),
        { input: 0, output: 0, tasks: 0 },
      ),
    [props.rows],
  )

  return (
    <box flexGrow={1} flexDirection="column" gap={1} paddingTop={1}>
      <text fg={theme.text.base} attributes={TextAttributes.BOLD}>
        Usage · last 7 days
      </text>
      {props.error ? (
        <text fg={theme.text.feedback.error.base}>{props.error}</text>
      ) : null}
      <text fg={theme.text.muted}>
        {formatTokens(totals.input)} in · {formatTokens(totals.output)} out · {totals.tasks} tasks
      </text>
      {props.rows.length > 0 ? (
        props.rows.map((row) => (
          <text key={`${row.agent_id}:${row.model}`} fg={theme.text.base}>
            {props.lookups.actorName("agent", row.agent_id) || row.agent_id.slice(0, 8)}
            {"  "}
            {row.model}
            {"  "}
            {formatTokens(row.input_tokens)}/{formatTokens(row.output_tokens)}
            {"  "}
            {row.task_count} tasks
          </text>
        ))
      ) : (
        <text fg={theme.text.muted}>No usage in this window.</text>
      )}
    </box>
  )
}
