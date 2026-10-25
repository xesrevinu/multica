import { createContext, useContext, type ReactNode } from "react"

/** Minimal theme tokens used by Multica TUI (hex / named colors OpenTUI accepts). */
export type ThemeColor = string

export type ComponentTheme = {
  text: {
    base: ThemeColor
    muted: ThemeColor
    action: { primary: { base: ThemeColor } }
    feedback: {
      error: { base: ThemeColor }
      warning: { base: ThemeColor }
      success: { base: ThemeColor }
      info: { base: ThemeColor }
    }
    formfield: { base: ThemeColor; disabled: ThemeColor }
  }
  background: {
    base: ThemeColor
    raised: { base: ThemeColor }
    formfield: { focused: ThemeColor; disabled: ThemeColor }
  }
  border: { base: ThemeColor }
  scrollbar: { base: ThemeColor }
  surface: (name: string) => ComponentTheme
}

const dark: Omit<ComponentTheme, "surface"> = {
  text: {
    base: "#cdd6f4",
    muted: "#6c7086",
    action: { primary: { base: "#89b4fa" } },
    feedback: {
      error: { base: "#f38ba8" },
      warning: { base: "#fab387" },
      success: { base: "#a6e3a1" },
      info: { base: "#89dceb" },
    },
    formfield: { base: "#cdd6f4", disabled: "#585b70" },
  },
  background: {
    base: "#1e1e2e",
    raised: { base: "#313244" },
    formfield: { focused: "#45475a", disabled: "#313244" },
  },
  border: { base: "#45475a" },
  scrollbar: { base: "#585b70" },
}

function withSurface(base: Omit<ComponentTheme, "surface">): ComponentTheme {
  const theme: ComponentTheme = {
    ...base,
    surface: () => theme,
  }
  return theme
}

export const defaultTheme = withSurface(dark)

const ThemeContext = createContext<ComponentTheme>(defaultTheme)

export function ThemeProvider(props: {
  children: ReactNode
  mode?: "dark" | "light"
  source?: unknown
}) {
  return <ThemeContext.Provider value={defaultTheme}>{props.children}</ThemeContext.Provider>
}

export function createThemeSource(_configDir: string) {
  return {}
}

export function useTheme() {
  return useContext(ThemeContext)
}
