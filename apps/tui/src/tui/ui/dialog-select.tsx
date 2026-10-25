import { useMemo, useState, type ReactNode } from "react"
import { TextAttributes } from "@opentui/core"
import { useKeyboard, useTerminalDimensions } from "@opentui/react"
import fuzzysort from "fuzzysort"
import { useTheme } from "../context/theme"
import { useDialog } from "./dialog"

export type DialogSelectOption<T = string> = {
  title: string
  value: T
  footer?: string
  category?: string
  searchText?: string
  description?: ReactNode
}

export type DialogSelectProps<T = string> = {
  title: string
  placeholder?: string
  options: DialogSelectOption<T>[]
  current?: T
  flat?: boolean
  sectionNavigation?: boolean
  onSelect: (option: DialogSelectOption<T>) => void
}

export function DialogSelect<T extends string>(props: DialogSelectProps<T>) {
  const theme = useTheme().surface("dialog")
  const dialog = useDialog()
  const dimensions = useTerminalDimensions()
  const [query, setQuery] = useState("")
  const [index, setIndex] = useState(0)

  const filtered = useMemo(() => {
    if (!query.trim()) return props.options
    const results = fuzzysort.go(query, props.options, {
      keys: ["title", "searchText", "footer", "category"],
      threshold: -20000,
    })
    return results.map((r) => r.obj)
  }, [props.options, query])

  const selected = filtered[Math.min(index, Math.max(filtered.length - 1, 0))]

  useKeyboard((evt) => {
    const name = (evt.name ?? "").toLowerCase()
    // j/k only navigate while the query is empty — otherwise they are text.
    const typing = query.length > 0
    if (name === "down" || (!typing && name === "j")) {
      evt.preventDefault?.()
      setIndex((i) => Math.min(i + 1, Math.max(filtered.length - 1, 0)))
      return
    }
    if (name === "up" || (!typing && name === "k")) {
      evt.preventDefault?.()
      setIndex((i) => Math.max(i - 1, 0))
      return
    }
    if (name === "return" || name === "enter") {
      evt.preventDefault?.()
      if (selected) props.onSelect(selected)
      return
    }
    if (name === "escape") {
      evt.preventDefault?.()
      dialog.clear()
      return
    }
    if (name === "backspace") {
      evt.preventDefault?.()
      setQuery((q) => q.slice(0, -1))
      setIndex(0)
      return
    }
    if (evt.ctrl || evt.meta) return
    if (evt.sequence && evt.sequence.length === 1 && !evt.sequence.match(/[\x00-\x1f]/)) {
      evt.preventDefault?.()
      setQuery((q) => q + evt.sequence)
      setIndex(0)
    }
  })

  const maxRows = Math.max(6, Math.min(16, dimensions.height - 10))

  return (
    <box paddingLeft={2} paddingRight={2} gap={1} flexDirection="column">
      <box flexDirection="row" justifyContent="space-between">
        <text fg={theme.text.base} attributes={TextAttributes.BOLD}>
          {props.title}
        </text>
        <text fg={theme.text.muted}>Esc</text>
      </box>
      <text fg={theme.text.muted}>
        {props.placeholder ?? "Search"}: {query || "…"}
      </text>
      <box flexDirection="column" height={maxRows}>
        {filtered.slice(0, maxRows).map((option, i) => {
          const active = i === Math.min(index, filtered.length - 1)
          return (
            <box key={`${option.value}-${i}`} flexDirection="row" gap={1}>
              <text fg={active ? theme.text.action.primary.base : theme.text.base}>
                {active ? "›" : " "}
              </text>
              <text fg={active ? theme.text.action.primary.base : theme.text.base}>
                {option.category ? `${option.category} · ` : ""}
                {option.title}
              </text>
              {option.footer ? <text fg={theme.text.muted}>{option.footer}</text> : null}
            </box>
          )
        })}
        {filtered.length === 0 ? <text fg={theme.text.muted}>No matches</text> : null}
      </box>
    </box>
  )
}
