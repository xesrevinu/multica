import { useEffect, useRef, useState } from "react"
import { TextAttributes, type TextareaRenderable } from "@opentui/core"
import { Keymap, useKeymapLayer } from "./context/keymap"
import { useConfig } from "./opentui-config"
import { useTheme } from "./context/theme"
import { useDialog, type DialogContext } from "./ui/dialog"
import { DialogPrompt } from "./ui/dialog-prompt"

export function openTextPrompt(
  dialog: DialogContext,
  options: {
    title: string
    placeholder?: string
    value?: string
    onSubmit: (value: string) => Promise<void>
  },
) {
  dialog.replace(() => <TextPromptDialog {...options} />)
}

function TextPromptDialog(options: {
  title: string
  placeholder?: string
  value?: string
  onSubmit: (value: string) => Promise<void>
}) {
  const dialog = useDialog()
  dialog.setCentered(true)
  const theme = useTheme().surface("dialog")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()

  const submit = async (value: string) => {
    const text = value.trim()
    if (!text) return
    setBusy(true)
    setError(undefined)
    try {
      await options.onSubmit(text)
      dialog.clear()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
      setBusy(false)
    }
  }

  return (
    <DialogPrompt
      title={options.title}
      placeholder={options.placeholder}
      value={options.value}
      busy={busy}
      busyText="Sending…"
      onConfirm={(value) => void submit(value)}
      onCancel={() => dialog.clear()}
      description={error ? <text fg={theme.text.feedback.error.base}>{error}</text> : undefined}
    />
  )
}

export function openComposeDialog(
  dialog: DialogContext,
  options: {
    title: string
    placeholder?: string
    value?: string
    onSubmit: (value: string) => Promise<void>
  },
) {
  dialog.replace(() => <ComposeDialog {...options} />)
}

function ComposeDialog(options: {
  title: string
  placeholder?: string
  value?: string
  onSubmit: (value: string) => Promise<void>
}) {
  const dialog = useDialog()
  dialog.setSize("large")
  dialog.setCentered(true)
  const theme = useTheme().surface("dialog")
  const config = useConfig().data
  const shortcuts = Keymap.useShortcuts()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()
  const textareaRef = useRef<TextareaRenderable | null>(null)

  const submit = async () => {
    const textarea = textareaRef.current
    if (!textarea || busy) return
    const text = textarea.plainText.trim()
    if (!text) return
    setBusy(true)
    setError(undefined)
    try {
      await options.onSubmit(text)
      dialog.clear()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
      setBusy(false)
    }
  }

  useKeymapLayer(() => ({
    mode: "modal",
    enabled: !!textareaRef.current && !busy,
    priority: 1,
    commands: [
      {
        id: "multica.compose.submit",
        bind: "return,enter",
        title: "Submit",
        group: "Compose",
        run: submit,
      },
    ],
  }))

  useEffect(() => {
    const id = setTimeout(() => {
      const textarea = textareaRef.current
      if (!textarea || textarea.isDestroyed || busy) return
      textarea.focus()
    }, 1)
    return () => clearTimeout(id)
  }, [busy])

  return (
    <box paddingLeft={2} paddingRight={2} gap={1} flexDirection="column">
      <box flexDirection="row" justifyContent="space-between">
        <text fg={theme.text.base} attributes={TextAttributes.BOLD}>{options.title}</text>
        <text fg={theme.text.muted}>Esc close</text>
      </box>
      <textarea
        height={6}
        wrapMode="word"
        ref={(val: TextareaRenderable | null) => {
          textareaRef.current = val
        }}
        placeholder={options.placeholder ?? "Write a comment…"}
        initialValue={options.value}
        placeholderColor={theme.text.muted}
        textColor={busy ? theme.text.formfield.disabled : theme.text.formfield.base}
        focusedTextColor={busy ? theme.text.formfield.disabled : theme.text.formfield.base}
        cursorColor={busy ? theme.background.formfield.disabled : theme.text.base}
        cursorStyle={{ style: config.cursor }}
      />
      {error ? (
        <text fg={theme.text.feedback.error.base}>{error}</text>
      ) : null}
      <text fg={theme.text.muted}>
        {shortcuts.get("multica.compose.submit") ?? "Enter"} submit · Esc cancel
      </text>
    </box>
  )
}
