import { describe, expect, test } from "bun:test"
import type { InboxItem } from "@multica/core/types"
import {
  deduplicateArchivedInboxItems,
  deduplicateInboxItems,
  filterInboxItems,
} from "../src/tui/inbox"

function item(partial: Partial<InboxItem> & Pick<InboxItem, "id">): InboxItem {
  return {
    workspace_id: "ws",
    recipient_type: "member",
    recipient_id: "u1",
    type: "new_comment",
    severity: "info",
    title: partial.title ?? partial.id,
    read: false,
    archived: false,
    created_at: "2026-01-02T00:00:00Z",
    issue_id: null,
    body: null,
    actor_type: null,
    actor_id: null,
    issue_status: null,
    issue_priority: null,
    details: null,
    ...partial,
  }
}

describe("inbox dedupe", () => {
  test("keeps one row per issue, newest first", () => {
    const rows = deduplicateInboxItems([
      item({ id: "a1", issue_id: "i1", title: "old", created_at: "2026-01-01T00:00:00Z" }),
      item({ id: "a2", issue_id: "i1", title: "new", created_at: "2026-01-03T00:00:00Z" }),
      item({ id: "b1", issue_id: "i2", title: "other", created_at: "2026-01-02T00:00:00Z" }),
    ])
    expect(rows.map((row) => row.id)).toEqual(["a2", "b1"])
  })

  test("drops archived from active and active from archived", () => {
    expect(
      deduplicateInboxItems([item({ id: "x", archived: true })]),
    ).toEqual([])
    expect(
      deduplicateArchivedInboxItems([
        item({ id: "x", archived: true }),
        item({ id: "y", archived: false }),
      ]).map((row) => row.id),
    ).toEqual(["x"])
  })

  test("filterInboxItems switches views", () => {
    const active = [
      item({ id: "u", read: false, title: "unread" }),
      item({ id: "r", read: true, title: "read" }),
    ]
    const archived = [item({ id: "z", archived: true, title: "done" })]
    expect(filterInboxItems(active, archived, "all").map((row) => row.id)).toEqual(["u", "r"])
    expect(filterInboxItems(active, archived, "unread").map((row) => row.id)).toEqual(["u"])
    expect(filterInboxItems(active, archived, "archived").map((row) => row.id)).toEqual(["z"])
  })
})
