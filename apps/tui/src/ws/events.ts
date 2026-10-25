export type WSEventType = string

export type TaskMessagePayload = {
  task_id: string
  issue_id: string
  seq: number
  type: "text" | "thinking" | "tool_use" | "tool_result" | "error"
  tool?: string
  content?: string
  output?: string
}


export type WSMessage = {
  type: WSEventType
  payload: unknown
  actor_id?: string
  actor_type?: string
}

export const ISSUE_EVENTS = [
  "issue:created",
  "issue:updated",
  "issue:deleted",
  "issue_metadata:changed",
  "issue_attachments:changed",
  "issue_labels:changed",
  "issue_properties:changed",
  "issue_status:changed",
] as const

export const COMMENT_EVENTS = [
  "comment:created",
  "comment:updated",
  "comment:deleted",
  "comment:resolved",
  "comment:unresolved",
] as const

export const TASK_EVENTS = [
  "task:queued",
  "task:dispatch",
  "task:running",
  "task:waiting_local_directory",
  "task:progress",
  "task:completed",
  "task:failed",
  "task:message",
  "task:cancelled",
] as const

export const CHAT_EVENTS = [
  "chat:message",
  "chat:done",
  "chat:quick_actions",
  "chat:cancel_finalized",
  "chat:session_created",
  "chat:session_read",
  "chat:session_deleted",
  "chat:session_updated",
] as const

export type ChatMessageEventPayload = {
  chat_session_id: string
  message_id: string
  role: "user" | "assistant"
  content: string
  task_id?: string
  created_at: string
}

export const WORKSPACE_REFRESH_PREFIXES = [
  "agent:",
  "inbox:",
  "project:",
  "label:",
  "activity:created",
  "chat:session_",
  "autopilot:",
] as const
