import {
  createContext,
  createElement,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ComponentType,
  type ReactNode,
} from "react"
import { RGBA } from "@opentui/core"
import { useKeyboard, useRenderer, useTerminalDimensions } from "@opentui/react"
import { useTheme } from "../context/theme"
import { FlushPendingKeymapLayers } from "../context/keymap"

export type DialogSize = "medium" | "large" | "xlarge"

export function dialogWidth(size: DialogSize) {
  if (size === "xlarge") return 116
  if (size === "large") return 88
  return 60
}

type StackItem = {
  element: ReactNode | ComponentType
  onClose?: () => void
  key?: unknown
}

type DialogApi = {
  clear: () => void
  replace: (
    input: ReactNode | ComponentType,
    onClose?: () => void,
    options?: { key?: unknown; size?: DialogSize },
  ) => void
  stack: StackItem[]
  size: DialogSize
  centered: boolean
  key: unknown
  setSize: (size: DialogSize) => void
  setCentered: (centered: boolean) => void
}

const DialogContext = createContext<DialogApi | null>(null)

export type DialogContext = DialogApi

function DialogFrame(props: {
  size: DialogSize
  centered: boolean
  onClose: () => void
  children: ReactNode
}) {
  const dimensions = useTerminalDimensions()
  const theme = useTheme().surface("dialog")
  const renderer = useRenderer()
  const dismiss = useRef(false)

  return (
    <box
      onMouseDown={() => {
        dismiss.current = !!renderer.getSelection()
      }}
      onMouseUp={() => {
        if (dismiss.current) {
          dismiss.current = false
          return
        }
        props.onClose()
      }}
      width={dimensions.width}
      height={dimensions.height}
      alignItems="center"
      justifyContent={props.centered ? "center" : undefined}
      position="absolute"
      zIndex={3000}
      paddingTop={props.centered ? 0 : Math.floor(dimensions.height / 4)}
      left={0}
      top={0}
      backgroundColor={RGBA.fromInts(0, 0, 0, 150)}
    >
      <box
        onMouseUp={(e: { stopPropagation(): void }) => {
          if (renderer.getSelection()?.getSelectedText()) return
          dismiss.current = false
          e.stopPropagation()
        }}
        width={dialogWidth(props.size)}
        maxWidth={dimensions.width - 2}
        backgroundColor={theme.background.base}
        paddingTop={1}
      >
        {props.children}
      </box>
    </box>
  )
}

export function DialogProvider(props: { children: ReactNode }) {
  const renderer = useRenderer()
  const [stack, setStack] = useState<StackItem[]>([])
  const [size, setSize] = useState<DialogSize>("medium")
  const [centered, setCentered] = useState(false)
  const focusRef = useRef<{ focus(): void; blur(): void; isDestroyed?: boolean } | null>(null)

  const clear = useCallback(() => {
    setStack((prev) => {
      for (const item of prev) item.onClose?.()
      return []
    })
    setSize("medium")
    setCentered(false)
    setTimeout(() => {
      const focus = focusRef.current
      if (focus && !focus.isDestroyed) focus.focus()
    }, 1)
  }, [])

  const replace = useCallback(
    (input: ReactNode | ComponentType, onClose?: () => void, options?: { key?: unknown; size?: DialogSize }) => {
      if (stack.length === 0) {
        focusRef.current = renderer.currentFocusedRenderable as typeof focusRef.current
        focusRef.current?.blur?.()
      }
      setStack((prev) => {
        for (const item of prev) item.onClose?.()
        return [{ element: input, onClose, key: options?.key }]
      })
      setSize(options?.size ?? "medium")
      setCentered(false)
    },
    [renderer, stack.length],
  )

  const value = useMemo<DialogApi>(
    () => ({
      clear,
      replace,
      stack,
      size,
      centered,
      key: stack.at(-1)?.key,
      setSize,
      setCentered,
    }),
    [clear, replace, stack, size, centered],
  )

  useKeyboard((evt) => {
    if (stack.length === 0) return
    const name = (evt.name ?? "").toLowerCase()
    if (name !== "escape" && !(evt.ctrl && name === "c")) return
    evt.preventDefault?.()
    if (renderer.getSelection()) {
      renderer.clearSelection()
      return
    }
    clear()
  })

  const top = stack.at(-1)
  const body =
    top == null
      ? null
      : typeof top.element === "function"
        ? createElement(top.element as ComponentType)
        : top.element

  return (
    <DialogContext.Provider value={value}>
      {props.children}
      {top ? (
        <DialogFrame onClose={clear} size={size} centered={centered}>
          <FlushPendingKeymapLayers />
          {body}
        </DialogFrame>
      ) : null}
    </DialogContext.Provider>
  )
}

export function useDialog() {
  const value = useContext(DialogContext)
  if (!value) throw new Error("useDialog must be used within a DialogProvider")
  return value
}

export function Dialog(props: {
  size?: DialogSize
  centered?: boolean
  onClose: () => void
  children: ReactNode
}) {
  return (
    <DialogFrame size={props.size ?? "medium"} centered={!!props.centered} onClose={props.onClose}>
      {props.children}
    </DialogFrame>
  )
}
