import { TextAttributes } from "@opentui/core"
import { useTerminalDimensions } from "@opentui/react"
import { useTheme } from "./context/theme"

export type Tone = "default" | "subdued" | "accent" | "success" | "warning" | "error" | "info"

export type Cell = {
  text: string
  tone?: Tone
  /** Fixed character width; the last cell without a width flexes. */
  width?: number
}

export type HeaderCell = {
  text: string
  width?: number
}

export type Row = {
  id: string
  cells: Cell[]
  /** Text the `/` filter matches against. */
  search: string
}

export function useTone() {
  const theme = useTheme()
  return (tone: Tone | undefined, selected: boolean) => {
    if (tone === "accent") return theme.text.action.primary.base
    if (tone === "success") return theme.text.feedback.success.base
    if (tone === "warning") return theme.text.feedback.warning.base
    if (tone === "error") return theme.text.feedback.error.base
    if (tone === "info") return theme.text.feedback.info.base
    if (tone === "subdued") return theme.text.muted
    return selected ? theme.text.action.primary.base : theme.text.base
  }
}

// Keeps the selected row inside a window of `height` rows.
export function windowRows<T extends { id: string }>(rows: T[], selectedId: string | undefined, height: number) {
  if (rows.length <= height) return rows
  const index = Math.max(0, rows.findIndex((row) => row.id === selectedId))
  const half = Math.floor(height / 2)
  const start = Math.min(Math.max(0, index - half), rows.length - height)
  return rows.slice(start, start + height)
}

export function Table(props: {
  headers?: HeaderCell[]
  rows: Row[]
  selectedId?: string
  onSelect: (id: string) => void
  height: number
}) {
  const theme = useTheme()
  const tone = useTone()
  const bodyHeight = Math.max(3, props.height - (props.headers ? 1 : 0))
  const visible = windowRows(props.rows, props.selectedId, bodyHeight)

  return (
    <box
      flexGrow={1}
      flexShrink={1}
      minHeight={0}
      flexDirection="column"
      onMouseScroll={(event) => {
        const direction = event.scroll?.direction
        if (direction !== "up" && direction !== "down") return
        const index = props.rows.findIndex((row) => row.id === props.selectedId)
        const next = props.rows[index + (direction === "down" ? 1 : -1)]
        if (next) props.onSelect(next.id)
      }}
    >
      {props.headers && props.rows.length > 0 ? (
        <box
          flexDirection="row"
          height={1}
          width="100%"
          flexShrink={0}
          gap={1}
          marginBottom={0}
        >
          <text width={2} fg={theme.text.muted}> </text>
          {props.headers.map((header) => (
            <text
              key={header.text}
              fg={theme.text.muted}
              width={header.width}
              flexGrow={header.width === undefined ? 1 : undefined}
              flexShrink={1}
              minWidth={0}
              wrapMode="none"
              truncate
              attributes={TextAttributes.BOLD}
            >
              {header.text}
            </text>
          ))}
        </box>
      ) : null}
      {props.rows.length > 0 ? (
        visible.map((row) => {
          const selected = row.id === props.selectedId
          return (
            <box
              key={row.id}
              flexDirection="row"
              height={1}
              width="100%"
              flexShrink={0}
              gap={1}
              backgroundColor={selected ? theme.background.raised.base : undefined}
              onMouseUp={() => props.onSelect(row.id)}
            >
              <text
                width={2}
                fg={selected ? theme.text.action.primary.base : theme.text.muted}
                attributes={selected ? TextAttributes.BOLD : undefined}
              >
                {selected ? "›" : " "}
              </text>
              {row.cells.map((cell, i) => (
                <text
                  key={i}
                  fg={tone(cell.tone, selected)}
                  width={cell.width}
                  flexGrow={cell.width === undefined ? 1 : undefined}
                  flexShrink={1}
                  minWidth={0}
                  wrapMode="none"
                  truncate
                  attributes={selected ? TextAttributes.BOLD : undefined}
                >
                  {cell.text}
                </text>
              ))}
            </box>
          )
        })
      ) : (
        <box paddingTop={1}>
          <text fg={theme.text.muted}>Nothing here.</text>
        </box>
      )}
    </box>
  )
}

export function useListHeight(reserved: number) {
  const dimensions = useTerminalDimensions()
  return Math.max(4, dimensions.height - reserved)
}
