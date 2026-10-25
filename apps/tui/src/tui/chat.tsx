import { useState, useEffect, useRef } from "react"
import { TextAttributes, type ScrollBoxRenderable } from "@opentui/core"
import { useKeyboard, useTerminalDimensions } from "@opentui/react"
import { useTheme } from "./context/theme"
import { useDialog } from "./ui/dialog"
import type { ChatMessage, ChatSession } from "../api/generated"
import type { DataClient } from "../data"
import type { RealtimeClient } from "../realtime"
import { formatRelative } from "../format"
import { openComposeDialog } from "./compose-dialog"
import { MarkdownView } from "./markdown-view"
import type { Lookups } from "./store"

export function ChatDetail(props: {
  id: string
  data: DataClient
  lookups: Lookups
  now: number
  realtime?: RealtimeClient
}) {
  const theme = useTheme()
  const dialog = useDialog()
  const dimensions = useTerminalDimensions()
  const [session, setSession] = useState<ChatSession>()
  const [messages, setMessages] = useState<readonly ChatMessage[]>([])
  const [error, setError] = useState<string>()
  const scrollRef = useRef<ScrollBoxRenderable | null>(null)

  const load = async () => {
    const [nextSession, nextMessages] = await Promise.all([
      props.data.getChatSession(props.id),
      props.data.listChatMessages(props.id),
    ])
    setSession(nextSession)
    setMessages(nextMessages.filter((message) => message.message_kind !== "onboarding_kickoff"))
    setError(undefined)
  }

  useEffect(() => {
    setSession(undefined)
    setMessages([])
    setError(undefined)
    void load().catch((reason) => setError(reason instanceof Error ? reason.message : String(reason)))
    const realtime = props.realtime
    if (!realtime) {
      const timer = setInterval(
        () => void load().catch((reason) => setError(reason instanceof Error ? reason.message : String(reason))),
        8_000,
      )
      return () => clearInterval(timer)
    }
    const off = realtime.onChat((type, payload) => {
      if (payload.chat_session_id !== props.id) return
      if (type === "chat:message") {
        setMessages((previous) => {
          if (previous.some((message) => message.id === payload.message_id)) return previous
          return [
            ...previous,
            {
              id: payload.message_id,
              chat_session_id: payload.chat_session_id,
              role: payload.role,
              content: payload.content,
              task_id: payload.task_id ?? null,
              created_at: payload.created_at,
            },
          ]
        })
        return
      }
      void load().catch((reason) => setError(reason instanceof Error ? reason.message : String(reason)))
    })
    return off
  }, [props.id, props.realtime])

  const scrollBy = (delta: number) => {
    const scroll = scrollRef.current
    if (!scroll) return
    if (delta < 0) scroll.stickyScroll = false
    scroll.scrollBy(delta)
  }

  useKeyboard((event) => {
    if (dialog.stack.length) return
    if (event.name === "c" && !event.ctrl && !event.shift) {
      openComposeDialog(dialog, {
        title: `Message ${session?.title || "chat"}`,
        placeholder: "Markdown supported…",
        onSubmit: async (content) => {
          await props.data.sendChatMessage(props.id, { content })
          await load()
        },
      })
      event.preventDefault()
      return
    }
    const scroll = scrollRef.current
    if (!scroll) return
    const page = Math.max(1, scroll.viewport.height - 1)
    if (event.name === "j" || event.name === "down") {
      scrollBy(1)
      event.preventDefault()
      return
    }
    if (event.name === "k" || event.name === "up") {
      scrollBy(-1)
      event.preventDefault()
      return
    }
    if (event.name === "pagedown") {
      scrollBy(page)
      event.preventDefault()
      return
    }
    if (event.name === "pageup") {
      scrollBy(-page)
      event.preventDefault()
    }
  })

  const width = Math.max(40, dimensions.width - 4)

  if (!session) {
    return (
      <box flexGrow={1} flexShrink={1} minHeight={0} flexDirection="column">
        <box paddingTop={1}>
          <text fg={error ? theme.text.feedback.error.base : theme.text.muted}>
            {error ?? "Loading chat…"}
          </text>
        </box>
      </box>
    )
  }

  return (
    <box flexGrow={1} flexShrink={1} minHeight={0} flexDirection="column">
      <scrollbox
        ref={(element: ScrollBoxRenderable) => {
          scrollRef.current = element
        }}
        flexGrow={1}
        flexShrink={1}
        minHeight={0}
        stickyScroll={true}
      >
        <box flexDirection="column" width={width} gap={1}>
          <box flexDirection="row" justifyContent="space-between">
            <box flexDirection="column" gap={0}>
              <text fg={theme.text.base} attributes={TextAttributes.BOLD}>
                {session.title || "Untitled chat"}
              </text>
              <text fg={theme.text.muted}>
                {props.lookups.actorName("agent", session.agent_id) || "agent"}
                {" · "}
                {session.status}
                {session.pinned ? " · pinned" : ""}
              </text>
            </box>
          </box>

          {error ? <text fg={theme.text.feedback.error.base}>{error}</text> : null}

          {messages.map((message) => {
            const isUser = message.role === "user"
            const kind = message.message_kind ?? "message"
            return (
              <box key={message.id} flexDirection="column" gap={0}>
                <box flexDirection="row" height={1} gap={1}>
                  <text
                    fg={isUser ? theme.text.base : theme.text.action.primary.base}
                    attributes={TextAttributes.BOLD}
                  >
                    {isUser ? "you" : props.lookups.actorName("agent", session.agent_id) || "assistant"}
                  </text>
                  <text fg={theme.text.muted} wrapMode="none">
                    {formatRelative(message.created_at, props.now)}
                    {kind !== "message" ? ` · ${kind}` : ""}
                    {message.failure_reason ? ` · ${message.failure_reason}` : ""}
                  </text>
                </box>
                {kind !== "no_response" ? (
                  <box
                    flexDirection="column"
                    backgroundColor={theme.background.raised.base}
                    paddingLeft={1}
                    paddingRight={1}
                  >
                    <MarkdownView source={message.content} width={width - 2} />
                  </box>
                ) : (
                  <text fg={theme.text.muted}>(no response)</text>
                )}
              </box>
            )
          })}

          {messages.length === 0 ? (
            <text fg={theme.text.muted}>No messages yet. Press c to send one.</text>
          ) : null}
        </box>
      </scrollbox>
    </box>
  )
}
