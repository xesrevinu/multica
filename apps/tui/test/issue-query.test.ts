import { describe, expect, test } from "bun:test"
import { buildIssueListParams } from "../src/tui/issue-query"
import { emptyStore } from "../src/tui/store"

describe("buildIssueListParams", () => {
  test("omits mine filters when scope is all", () => {
    const params = buildIssueListParams(emptyStore, {
      offset: 0,
      mine: "all",
      userID: "u1",
    })
    expect(params.assignee_id).toBeUndefined()
    expect(params.creator_id).toBeUndefined()
    expect(params.involves_user_id).toBeUndefined()
  })

  test("maps assigned / created / agents to API params", () => {
    expect(
      buildIssueListParams(emptyStore, { offset: 0, mine: "assigned", userID: "u1" }).assignee_id,
    ).toBe("u1")
    expect(
      buildIssueListParams(emptyStore, { offset: 0, mine: "created", userID: "u1" }).creator_id,
    ).toBe("u1")
    expect(
      buildIssueListParams(emptyStore, { offset: 0, mine: "agents", userID: "u1" }).involves_user_id,
    ).toBe("u1")
  })
})
