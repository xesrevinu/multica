import type { TimelineEntry } from "../api/generated"
import type { Lookups } from "./store"

export function formatActivity(
  entry: TimelineEntry,
  lookups: Lookups,
): string {
  const details = (entry.details ?? {}) as Record<string, string>
  switch (entry.action) {
    case "created":
      return "created this issue"
    case "status_changed":
      return `status ${details.from ?? "?"} → ${details.to ?? "?"}`
    case "priority_changed":
      return `priority ${details.from ?? "?"} → ${details.to ?? "?"}`
    case "assignee_changed": {
      if (details.to_type && details.to_id) {
        const name = lookups.actorName(details.to_type, details.to_id)
        return name ? `assigned to ${name}` : "changed assignee"
      }
      if (details.from_id && !details.to_id) return "removed assignee"
      return "changed assignee"
    }
    case "start_date_changed":
      return details.to ? `start date → ${details.to}` : "removed start date"
    case "due_date_changed":
      return details.to ? `due date → ${details.to}` : "removed due date"
    case "title_changed":
      return `renamed "${details.from ?? "?"}" → "${details.to ?? "?"}"`
    case "description_updated":
      return "updated description"
    case "task_completed":
      return "agent task completed"
    case "task_failed":
      return "agent task failed"
    case "squad_leader_evaluated":
      return details.outcome ? `squad leader: ${details.outcome}` : "squad leader evaluated"
    default:
      return entry.action ?? "updated"
  }
}
