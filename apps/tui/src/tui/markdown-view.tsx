import { useMemo } from "react"
import { TextAttributes } from "@opentui/core"
import { useTheme } from "./context/theme"
import { parseMarkdown, type MarkdownBlock } from "../markdown"

function BlockView(props: { block: MarkdownBlock; width?: number }) {
  const theme = useTheme()

  if (props.block.kind === "heading") {
    return (
      <text fg={theme.text.base} attributes={TextAttributes.BOLD} wrapMode="word">
        {props.block.text}
      </text>
    )
  }

  if (props.block.kind === "code") {
    return (
      <box
        flexDirection="column"
        backgroundColor={theme.background.raised.base}
        paddingLeft={1}
        paddingRight={1}
      >
        {props.block.lang ? (
          <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>{props.block.lang}</text>
        ) : null}
        {props.block.lines.map((line, i) => (
          <text key={i} fg={theme.text.base} wrapMode="word">{line}</text>
        ))}
      </box>
    )
  }

  if (props.block.kind === "list") {
    const ordered = props.block.ordered
    return (
      <box flexDirection="column" gap={0}>
        {props.block.items.map((item, i) => (
          <text key={i} fg={theme.text.base} wrapMode="word">
            {ordered ? `${i + 1}. ` : "• "}
            {item}
          </text>
        ))}
      </box>
    )
  }

  return (
    <text fg={theme.text.base} wrapMode="word">
      {props.block.text}
    </text>
  )
}

export function MarkdownView(props: { source: string; width?: number }) {
  const blocks = useMemo(() => parseMarkdown(props.source), [props.source])

  return (
    <box flexDirection="column" gap={0} width={props.width}>
      {blocks.map((block, i) => (
        <BlockView key={i} block={block} width={props.width} />
      ))}
    </box>
  )
}
