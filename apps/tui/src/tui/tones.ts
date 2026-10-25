import type { AgentTaskStatus, IssuePriority, IssueStatusCategory } from "../api/generated"
import type { Tone } from "./table"

export const CATEGORY_TONE: Record<IssueStatusCategory, Tone> = {
  backlog: "subdued",
  todo: "default",
  in_progress: "accent",
  in_review: "info",
  done: "success",
  blocked: "warning",
  cancelled: "subdued",
}

export const PRIORITY_TONE: Record<IssuePriority, Tone> = {
  urgent: "error",
  high: "warning",
  medium: "default",
  low: "subdued",
  none: "subdued",
}

export const AGENT_STATUS_TONE: Record<string, Tone> = {
  idle: "subdued",
  working: "success",
  blocked: "warning",
  error: "error",
  offline: "subdued",
}

export const TASK_STATUS_TONE: Record<AgentTaskStatus, Tone> = {
  queued: "info",
  dispatched: "info",
  waiting_local_directory: "warning",
  running: "accent",
  completed: "success",
  failed: "error",
  cancelled: "subdued",
}

export const PROJECT_STATUS_TONE: Record<string, Tone> = {
  planned: "default",
  in_progress: "accent",
  paused: "warning",
  completed: "success",
  cancelled: "subdued",
}
