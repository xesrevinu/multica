import { createContext, useContext, type ReactNode } from "react"

/** Slim CLI config shape used by Multica TUI (subset of OpenCode Info). */
export type Info = {
  mouse?: boolean
  cursor?: "block" | "underline" | "bar" | "line"
  terminal?: {
    copy?: "select" | "manual"
  }
}

export type Interface = {
  path?: string
  get: () => Promise<Info>
  update: (update: (current: Info) => void) => Promise<Info>
}

export type Resolved = Info & {
  mouse: boolean
  cursor: "block" | "underline" | "line"
}

export function resolve(info: Info, options?: { terminalSuspend?: boolean }): Resolved {
  const raw = info.cursor ?? "block"
  return {
    ...info,
    mouse: info.mouse ?? true,
    cursor: raw === "bar" ? "line" : raw,
  }
}

type ConfigValue = {
  data: Resolved
  service: Interface
}

const ConfigContext = createContext<ConfigValue | null>(null)

export function ConfigProvider(props: {
  config: Resolved
  service: Interface
  options?: { terminalSuspend?: boolean }
  children: ReactNode
}) {
  return (
    <ConfigContext.Provider value={{ data: props.config, service: props.service }}>
      {props.children}
    </ConfigContext.Provider>
  )
}

export function useConfig() {
  const value = useContext(ConfigContext)
  if (!value) throw new Error("useConfig must be used within ConfigProvider")
  return value
}
