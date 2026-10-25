import { TextAttributes } from "@opentui/core"
import { useTheme } from "./context/theme"
import type { Connection } from "../config"
import { sessionStatePath } from "./session-state"
import type { Store } from "./store"

export function SettingsView(props: { store: Store; connection: Connection }) {
  const theme = useTheme()
  const workspace = props.store.workspace
  const me = props.store.me

  return (
    <box flexGrow={1} flexDirection="column" gap={1} paddingTop={1}>
      <text fg={theme.text.base} attributes={TextAttributes.BOLD}>Settings</text>
      <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>account</text>
      <text fg={theme.text.base}>{me?.name ?? "—"}</text>
      <text fg={theme.text.muted}>{me?.email ?? ""}</text>
      <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>workspace</text>
      <text fg={theme.text.base}>
        {workspace?.name ?? "—"}
        {workspace?.issue_prefix ? ` · ${workspace.issue_prefix}` : ""}
      </text>
      <text fg={theme.text.muted}>{workspace?.slug ?? props.connection.workspaceID}</text>
      <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>connection</text>
      <text fg={theme.text.muted}>{props.connection.serverUrl}</text>
      <text fg={theme.text.muted}>source {props.connection.source}</text>
      <text fg={theme.text.muted}>view {sessionStatePath(props.connection.source)}</text>
      <text fg={theme.text.muted}>p project · Ctrl-w workspace · g go</text>
    </box>
  )
}
