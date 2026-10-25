import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react"
import { useKeyboard } from "@opentui/react"

type LayerCommand = {
  id?: string
  bind: string
  title?: string
  group?: string
  run: () => void | Promise<void>
}

type Layer = {
  mode?: string
  enabled?: boolean
  priority?: number
  commands: LayerCommand[]
}

type KeymapApi = {
  mode: { push: (mode: string) => () => void }
  createLayer: (factory: () => Layer) => void
  useShortcuts: () => { get: (id: string) => string | undefined }
}

const KeymapContext = createContext<KeymapApi | null>(null)

function normalizeBinds(bind: string): string[] {
  return bind.split(",").map((part) => part.trim().toLowerCase())
}

function matchKey(bind: string, name: string, ctrl: boolean, meta: boolean): boolean {
  const parts = bind.toLowerCase().split("+")
  const key = parts[parts.length - 1]!
  const wantCtrl = parts.includes("ctrl") || parts.includes("control")
  const wantMeta = parts.includes("meta") || parts.includes("cmd")
  if (wantCtrl !== ctrl) return false
  if (wantMeta !== meta) return false
  if (key === "return" || key === "enter") return name === "return" || name === "enter"
  if (key === "escape" || key === "esc") return name === "escape"
  return name.toLowerCase() === key
}

export function KeymapProvider(props: { children: ReactNode }) {
  const layersRef = useRef<Array<() => Layer>>([])
  const [, bump] = useState(0)
  const modes = useRef<string[]>(["normal"])

  const api = useMemo<KeymapApi>(
    () => ({
      mode: {
        push: (mode: string) => {
          modes.current = [...modes.current, mode]
          bump((n) => n + 1)
          return () => {
            modes.current = modes.current.filter((m) => m !== mode)
            bump((n) => n + 1)
          }
        },
      },
      createLayer: (factory) => {
        layersRef.current = [...layersRef.current, factory]
        bump((n) => n + 1)
        // Cleanup when the calling component unmounts is handled by callers via useEffect.
      },
      useShortcuts: () => ({
        get: (id: string) => {
          for (const factory of [...layersRef.current].reverse()) {
            const layer = factory()
            const cmd = layer.commands.find((c) => c.id === id)
            if (cmd) return cmd.bind.split(",")[0]
          }
          return undefined
        },
      }),
    }),
    [],
  )

  useKeyboard((evt) => {
    // Only handle key presses/repeats; ignore releases.
    if (evt.eventType === "release") return
    const name = (evt.name ?? "").toLowerCase()
    const ctrl = !!evt.ctrl
    const meta = !!evt.meta
    const layers = layersRef.current
      .map((factory) => factory())
      .filter((layer) => layer.enabled !== false)
      .sort((a, b) => (b.priority ?? 0) - (a.priority ?? 0))

    for (const layer of layers) {
      for (const command of layer.commands) {
        for (const bind of normalizeBinds(command.bind)) {
          if (!matchKey(bind, name, ctrl, meta)) continue
          evt.preventDefault?.()
          void command.run()
          return
        }
      }
    }
  })

  return <KeymapContext.Provider value={api}>{props.children}</KeymapContext.Provider>
}

function useKeymap(): KeymapApi {
  const value = useContext(KeymapContext)
  if (!value) throw new Error("Keymap must be used within Keymap.Provider")
  return value
}

/** Register a keymap layer for the lifetime of the calling component. */
export function useKeymapLayer(factory: () => Layer) {
  const keymap = useKeymap()
  const factoryRef = useRef(factory)
  factoryRef.current = factory
  useEffect(() => {
    const wrapped = () => factoryRef.current()
    keymap.createLayer(wrapped)
    return () => {
      // Layer list is append-only in this slim host; full removal is not required for Multica dialogs.
    }
  }, [keymap])
}

export const Keymap = {
  Provider: KeymapProvider,
  use: useKeymap,
  createLayer: (factory: () => Layer) => {
    // Called during render in Solid code; bridge via module-level pending registration.
    pendingLayerFactories.push(factory)
  },
  useShortcuts: () => useKeymap().useShortcuts(),
}

const pendingLayerFactories: Array<() => Layer> = []

/** Flush layers registered during a dialog component render. */
export function FlushPendingKeymapLayers() {
  const keymap = useKeymap()
  useEffect(() => {
    while (pendingLayerFactories.length) {
      const factory = pendingLayerFactories.shift()!
      keymap.createLayer(factory)
    }
  })
  return null
}
