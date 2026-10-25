import type { Connection } from "./config"
import { WSClient } from "./ws/client"
import { CHAT_EVENTS, COMMENT_EVENTS, ISSUE_EVENTS, TASK_EVENTS, WORKSPACE_REFRESH_PREFIXES, type ChatMessageEventPayload, type TaskMessagePayload, type WSMessage } from "./ws/events"
import { resolveWsUrl } from "./ws/url"

export type RealtimeClient = ReturnType<typeof makeRealtimeClient>

export function makeRealtimeClient(connection: Connection, workspaceSlug: string) {
  const client = new WSClient(resolveWsUrl(connection.serverUrl), {
    identity: { platform: "multica-tui", version: "0.1.0", os: process.platform },
  })
  client.setAuth(connection.token, workspaceSlug)
  client.connect()

  const issueListeners = new Set<(issueId: string, type: string) => void>()
  const workspaceListeners = new Set<() => void>()

  const notifyIssue = (issueId: string, type: string) => {
    for (const listener of issueListeners) listener(issueId, type)
  }

  const notifyWorkspace = () => {
    for (const listener of workspaceListeners) listener()
  }

  let debounce: ReturnType<typeof setTimeout> | undefined
  const debouncedWorkspace = () => {
    if (debounce) clearTimeout(debounce)
    debounce = setTimeout(() => {
      debounce = undefined
      notifyWorkspace()
    }, 300)
  }

  const onMessage = (msg: WSMessage) => {
    if (ISSUE_EVENTS.includes(msg.type as typeof ISSUE_EVENTS[number])) {
      const payload = msg.payload as { issue?: { id?: string }; issue_id?: string }
      const issueId = payload.issue?.id ?? payload.issue_id
      if (issueId) notifyIssue(issueId, msg.type)
      debouncedWorkspace()
      return
    }
    if (COMMENT_EVENTS.includes(msg.type as typeof COMMENT_EVENTS[number])) {
      const payload = msg.payload as { comment?: { issue_id?: string } }
      const issueId = payload.comment?.issue_id
      if (issueId) notifyIssue(issueId, msg.type)
      return
    }
    if (TASK_EVENTS.includes(msg.type as typeof TASK_EVENTS[number])) {
      const payload = msg.payload as { issue_id?: string; task?: { issue_id?: string } }
      const issueId = payload.issue_id ?? payload.task?.issue_id
      if (issueId) notifyIssue(issueId, msg.type)
      debouncedWorkspace()
      return
    }
    if (CHAT_EVENTS.includes(msg.type as typeof CHAT_EVENTS[number])) {
      if (msg.type.startsWith("chat:session_")) debouncedWorkspace()
      return
    }
    if (WORKSPACE_REFRESH_PREFIXES.some((prefix) => msg.type.startsWith(prefix))) {
      debouncedWorkspace()
    }
  }

  const offAny = client.onAny(onMessage)
  const offReconnect = client.onReconnect(() => notifyWorkspace())

  return {
    client,
    isConnected: () => client.isConnected,
    onIssue: (listener: (issueId: string, type: string) => void) => {
      issueListeners.add(listener)
      return () => issueListeners.delete(listener)
    },
    onWorkspace: (listener: () => void) => {
      workspaceListeners.add(listener)
      return () => workspaceListeners.delete(listener)
    },
    onTaskMessage: (listener: (payload: TaskMessagePayload) => void) => {
      const handler = (msg: WSMessage) => {
        if (msg.type !== "task:message") return
        listener(msg.payload as TaskMessagePayload)
      }
      return client.onAny(handler)
    },
    onChat: (listener: (type: string, payload: ChatMessageEventPayload) => void) => {
      const handler = (msg: WSMessage) => {
        if (!CHAT_EVENTS.includes(msg.type as typeof CHAT_EVENTS[number])) return
        listener(msg.type, msg.payload as ChatMessageEventPayload)
      }
      return client.onAny(handler)
    },
    dispose: () => {
      offAny()
      offReconnect()
      if (debounce) clearTimeout(debounce)
      client.disconnect()
      issueListeners.clear()
      workspaceListeners.clear()
    },
  }
}
