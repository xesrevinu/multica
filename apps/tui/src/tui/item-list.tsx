import { TextAttributes } from "@opentui/core"
import { useTheme } from "./context/theme"
import type { Tone } from "./table"
import { useTone } from "./table"

export type ItemListRow = {
  id: string
  cells: { text: string; width?: number; tone?: Tone }[]
  lines?: string[]
}

export function ItemList(props: {
  title?: string
  rows: ItemListRow[]
  empty?: string
  tone?: Tone
  onRowSelect?: (id: string) => void
}) {
  const theme = useTheme()
  const tone = useTone()

  return (
    <box flexDirection="column" gap={0}>
      {props.title ? (
        <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>
          {props.title} ({props.rows.length})
        </text>
      ) : null}
      {props.rows.length > 0 ? (
        props.rows.map((row) => (
          <box key={row.id} flexDirection="column" marginBottom={0} gap={0}>
            <box
              flexDirection="row"
              height={1}
              gap={1}
              onMouseUp={() => props.onRowSelect?.(row.id)}
            >
              {row.cells.map((cell, i) => (
                <text
                  key={i}
                  fg={tone(cell.tone ?? props.tone, false)}
                  width={cell.width}
                  wrapMode="none"
                  truncate={cell.width !== undefined}
                >
                  {cell.text}
                </text>
              ))}
            </box>
            {row.lines && row.lines.length > 0
              ? row.lines.map((line, i) => (
                  <text key={i} fg={theme.text.muted} wrapMode="word">
                    {"  "}{line}
                  </text>
                ))
              : null}
          </box>
        ))
      ) : (
        <text fg={theme.text.muted}>{props.empty ?? "None"}</text>
      )}
    </box>
  )
}
