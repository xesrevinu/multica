import { createContext, useContext, type ReactNode } from "react"

export type ToastOptions = {
  title?: string
  message?: string
  variant?: "info" | "success" | "warning" | "error"
}

type ToastApi = {
  show: (options: ToastOptions) => void
}

const ToastContext = createContext<ToastApi>({
  show: () => {},
})

export function ToastProvider(props: { children: ReactNode }) {
  return <ToastContext.Provider value={{ show: () => {} }}>{props.children}</ToastContext.Provider>
}

export function useToast() {
  return useContext(ToastContext)
}
