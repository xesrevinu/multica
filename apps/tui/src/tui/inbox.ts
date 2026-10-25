import type { InboxItem } from "@multica/core/types"
import {
  deduplicateArchivedInboxItems,
  deduplicateInboxItems,
} from "@multica/core/inbox/queries"

export { deduplicateArchivedInboxItems, deduplicateInboxItems }

export type InboxView = "all" | "unread" | "archived"

export const INBOX_VIEWS = ["all", "unread", "archived"] as const

/** TUI view switcher — not the same as core InboxFilters.filterInboxItems. */
export function filterInboxItems(
  items: readonly InboxItem[],
  archived: readonly InboxItem[],
  view: InboxView,
): InboxItem[] {
  if (view === "archived") return deduplicateArchivedInboxItems([...archived])
  const active = deduplicateInboxItems([...items])
  if (view === "unread") return active.filter((item) => !item.read)
  return active
}
