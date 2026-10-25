import { type ReactNode } from "react"
import { useTheme } from "./context/theme"

export function SurfaceStrip(props: {
  children: ReactNode
  tone?: "default" | "raised" | "inset"
  flexShrink?: number
  flexGrow?: number
  minHeight?: number
  height?: number
  paddingLeft?: number
  paddingRight?: number
}) {
  const theme = useTheme()
  let background: string | undefined
  if (props.tone === "raised") background = theme.background.raised.base
  else if (props.tone === "inset") background = theme.background.raised.base

  return (
    <box
      flexShrink={props.flexShrink}
      flexGrow={props.flexGrow}
      minHeight={props.minHeight}
      height={props.height}
      paddingLeft={props.paddingLeft ?? 1}
      paddingRight={props.paddingRight ?? 1}
      backgroundColor={background}
      flexDirection="column"
    >
      {props.children}
    </box>
  )
}
