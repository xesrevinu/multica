import { TextAttributes } from "@opentui/core"
import { useTheme } from "./context/theme"
import type { TimelineEntry } from "../api/generated"
import { formatRelative } from "../format"
import { formatActivity } from "./activity"
import { MarkdownView } from "./markdown-view"
import type { Lookups } from "./store"

export function TimelineView(props: {
  entries: readonly TimelineEntry[]
  lookups: Lookups
  now: number
  width: number
  selectedCommentId?: string
  onSelectComment?: (id: string) => void
}) {
  const theme = useTheme()

  return (
    <box flexDirection="column" gap={1}>
      {props.entries.map((entry) => {
        const selected = entry.type === "comment" && props.selectedCommentId === entry.id
        return (
          <box
            key={entry.id}
            id={`timeline-${entry.id}`}
            flexDirection="column"
            gap={0}
            onMouseUp={() => {
              if (entry.type === "comment") props.onSelectComment?.(entry.id)
            }}
          >
            {entry.type === "comment" ? (
              <box flexDirection="row" height={1} gap={1} paddingLeft={entry.parent_id ? 2 : 0}>
                <text
                  fg={
                    entry.actor_type === "agent"
                      ? theme.text.action.primary.base
                      : theme.text.base
                  }
                  attributes={TextAttributes.BOLD}
                >
                  {entry.actor_name ??
                    props.lookups.actorName(entry.actor_type, entry.actor_id) ??
                    entry.actor_type}
                </text>
                <text fg={theme.text.muted} wrapMode="none">
                  {formatRelative(entry.created_at, props.now)}
                  {entry.parent_id ? " · reply" : ""}
                  {entry.comment_type && entry.comment_type !== "comment" ? ` · ${entry.comment_type}` : ""}
                  {entry.resolved_at ? " · resolved" : ""}
                  {selected ? " · selected" : ""}
                </text>
              </box>
            ) : (
              <box flexDirection="row" height={1} gap={1}>
                <text fg={theme.text.feedback.info.base} attributes={TextAttributes.BOLD}>
                  {entry.actor_name ??
                    props.lookups.actorName(entry.actor_type, entry.actor_id) ??
                    entry.actor_type}
                </text>
                <text fg={theme.text.muted} wrapMode="none">
                  {formatRelative(entry.created_at, props.now)} · activity
                </text>
              </box>
            )}

            {entry.type === "activity" ? (
              <text fg={theme.text.muted} wrapMode="word">
                {formatActivity(entry, props.lookups)}
              </text>
            ) : null}

            {entry.type === "comment" && entry.content ? (
              <box
                flexDirection="column"
                backgroundColor={selected ? theme.background.formfield.focused : theme.background.raised.base}
                paddingLeft={entry.parent_id ? 3 : 1}
                paddingRight={1}
                marginTop={0}
              >
                <MarkdownView source={entry.content} width={Math.max(20, props.width - (entry.parent_id ? 4 : 2))} />
              </box>
            ) : null}
          </box>
        )
      })}
    </box>
  )
}
