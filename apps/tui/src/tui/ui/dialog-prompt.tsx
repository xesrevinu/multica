import { useEffect, useRef, useState, type ReactNode } from "react"
import { TextAttributes, type InputRenderable } from "@opentui/core"
import { useTheme } from "../context/theme"

export type DialogPromptProps = {
  title: string
  placeholder?: string
  value?: string
  busy?: boolean
  busyText?: string
  onConfirm: (value: string) => void
  onCancel: () => void
  description?: ReactNode | (() => ReactNode)
}

export function DialogPrompt(props: DialogPromptProps) {
  const theme = useTheme().surface("dialog")
  const [value, setValue] = useState(props.value ?? "")
  const inputRef = useRef<InputRenderable | null>(null)

  useEffect(() => {
    const id = setTimeout(() => inputRef.current?.focus(), 1)
    return () => clearTimeout(id)
  }, [])

  const description =
    typeof props.description === "function" ? props.description() : props.description

  return (
    <box paddingLeft={2} paddingRight={2} gap={1} flexDirection="column">
      <text fg={theme.text.base} attributes={TextAttributes.BOLD}>
        {props.title}
      </text>
      {description}
      <input
        ref={(node: InputRenderable | null) => {
          inputRef.current = node
        }}
        focused
        value={value}
        placeholder={props.placeholder}
        onInput={(v: string) => setValue(v)}
        onSubmit={() => {
          if (props.busy) return
          props.onConfirm(value)
        }}
        onKeyDown={(key: { name: string }) => {
          if (key.name === "escape") props.onCancel()
        }}
        textColor={props.busy ? theme.text.formfield.disabled : theme.text.formfield.base}
        focusedTextColor={props.busy ? theme.text.formfield.disabled : theme.text.formfield.base}
        placeholderColor={theme.text.muted}
        cursorColor={theme.text.base}
      />
      <text fg={theme.text.muted}>
        {props.busy ? (props.busyText ?? "Working…") : "Enter confirm · Esc cancel"}
      </text>
    </box>
  )
}
