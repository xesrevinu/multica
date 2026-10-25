import { useQuery } from "@tanstack/react-query"
import {
  archivedInboxListOptions,
  deduplicateInboxItems,
  inboxListOptions,
} from "@multica/core/inbox/queries"
import {
  useArchiveAllInbox,
  useArchiveAllReadInbox,
  useArchiveCompletedInbox,
  useArchiveInbox,
  useMarkAllInboxRead,
  useMarkInboxRead,
  useMarkInboxUnread,
  useUnarchiveInbox,
} from "@multica/core/inbox/mutations"
import type { InboxItem } from "@multica/core/types"
import { filterInboxItems, type InboxView } from "./inbox"

/**
 * Shared inbox transport + React Query (same options/mutations as Web).
 * TUI only adds the OpenTUI view filter (all / unread / archived).
 */
export function useSharedInbox(workspaceId: string, view: InboxView) {
  const listQ = useQuery({
    ...inboxListOptions(workspaceId),
    enabled: !!workspaceId,
    refetchInterval: 15_000,
  })
  const archivedQ = useQuery({
    ...archivedInboxListOptions(workspaceId),
    enabled: !!workspaceId,
    refetchInterval: 30_000,
  })

  const active: readonly InboxItem[] = listQ.data ?? []
  const archived: readonly InboxItem[] = archivedQ.data ?? []
  const items = filterInboxItems(active, archived, view)
  const unreadCount = deduplicateInboxItems([...active]).filter((item) => !item.read).length

  return {
    active,
    archived,
    items,
    unreadCount,
    isLoading: listQ.isLoading || archivedQ.isLoading,
    error: listQ.error ?? archivedQ.error,
    markRead: useMarkInboxRead(),
    markUnread: useMarkInboxUnread(),
    markAllRead: useMarkAllInboxRead(),
    archive: useArchiveInbox(),
    unarchive: useUnarchiveInbox(),
    archiveAll: useArchiveAllInbox(),
    archiveAllRead: useArchiveAllReadInbox(),
    archiveCompleted: useArchiveCompletedInbox(),
  }
}
