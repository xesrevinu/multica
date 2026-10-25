import { createContext, useContext, type ReactNode } from "react"

export type Clipboard = {
  read: () => Promise<string | undefined>
  write: (text: string) => Promise<void>
}

const ClipboardContext = createContext<Clipboard>({
  read: async () => undefined,
  write: async () => {},
})

export function ClipboardProvider(props: { value: Clipboard; children: ReactNode }) {
  return <ClipboardContext.Provider value={props.value}>{props.children}</ClipboardContext.Provider>
}

export function useClipboard() {
  return useContext(ClipboardContext)
}
