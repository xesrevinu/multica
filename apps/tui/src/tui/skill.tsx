import { useState, useEffect, useRef } from "react"
import { TextAttributes, type ScrollBoxRenderable } from "@opentui/core"
import { useKeyboard, useTerminalDimensions } from "@opentui/react"
import { useTheme } from "./context/theme"
import { useDialog } from "./ui/dialog"
import type { SkillWithFiles } from "../api/generated"
import type { DataClient } from "../data"
import { MarkdownView } from "./markdown-view"

export function SkillDetail(props: {
  id: string
  data: DataClient
  height: number
}) {
  const theme = useTheme()
  const dialog = useDialog()
  const dimensions = useTerminalDimensions()
  const [skill, setSkill] = useState<SkillWithFiles>()
  const [error, setError] = useState<string>()
  const scrollRef = useRef<ScrollBoxRenderable | null>(null)

  useEffect(() => {
    setSkill(undefined)
    setError(undefined)
    void props.data
      .getSkill(props.id)
      .then(setSkill)
      .catch((reason) => setError(reason instanceof Error ? reason.message : String(reason)))
  }, [props.id, props.data])

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

  if (!skill) {
    return (
      <box flexGrow={1} flexShrink={1} minHeight={0} flexDirection="column">
        <box paddingTop={1}>
          <text fg={error ? theme.text.feedback.error.base : theme.text.muted}>
            {error ?? "Loading skill…"}
          </text>
        </box>
      </box>
    )
  }

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
          <text fg={theme.text.base} attributes={TextAttributes.BOLD}>
            {skill.name}
          </text>
          {skill.description ? (
            <text fg={theme.text.muted} wrapMode="word">
              {skill.description}
            </text>
          ) : null}
          {(skill.files ?? []).map((file) => (
            <box key={file.path} flexDirection="column" gap={0}>
              <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>
                {file.path}
              </text>
              <MarkdownView source={file.content} width={width} />
            </box>
          ))}
        </box>
      </scrollbox>
    </box>
  )
}
