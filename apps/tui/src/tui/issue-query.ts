import type { Store } from "./store"

export const ISSUE_PAGE_SIZE = 50

/** Matches web my-issues scopes (agents → involves_user_id). */
export type IssueMineScope = "all" | "assigned" | "created" | "agents"

export const ISSUE_MINE_SCOPES = ["all", "assigned", "created", "agents"] as const

export function buildIssueListParams(
  store: Store,
  options: {
    offset: number
    limit?: number
    query?: string
    statusCategory?: string
    projectID?: string
    mine?: IssueMineScope
    userID?: string
  },
) {
  const statuses =
    options.statusCategory && options.statusCategory !== "all"
      ? store.statuses
          .filter((status) => !status.archived_at && status.category === options.statusCategory)
          .map((status) => status.key)
          .join(",")
      : undefined

  const mine = mineParams(options.mine, options.userID)

  return {
    limit: options.limit ?? ISSUE_PAGE_SIZE,
    offset: options.offset,
    sort_by: "updated_at",
    sort_direction: "desc" as const,
    q: options.query?.trim() || undefined,
    project_id: options.projectID && options.projectID !== "all" ? options.projectID : undefined,
    statuses: statuses || undefined,
    ...mine,
  }
}

function mineParams(mine: IssueMineScope | undefined, userID: string | undefined) {
  if (!mine || mine === "all" || !userID) return {}
  if (mine === "assigned") return { assignee_id: userID }
  if (mine === "created") return { creator_id: userID }
  return { involves_user_id: userID }
}
