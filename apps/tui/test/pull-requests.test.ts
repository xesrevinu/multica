import { describe, expect, test } from "bun:test"
import { decodeIssuePullRequests } from "../src/pull-requests"

const pull = {
  id: "pr-1",
  provider: "github",
  workspace_id: "ws",
  repo_owner: "anomalyco",
  repo_name: "opencode",
  number: 540,
  title: "Fix the scroll",
  state: "closed",
  html_url: "https://github.com/anomalyco/opencode/pull/540",
  branch: "fix-scroll",
  author_login: "ada",
  author_avatar_url: null,
  merged_at: "2026-08-01T00:00:00Z",
  closed_at: "2026-08-01T00:00:00Z",
  pr_created_at: "2026-07-01T00:00:00Z",
  pr_updated_at: "2026-08-01T00:00:00Z",
  checks_total: 3,
  additions: 10,
}

describe("decodeIssuePullRequests", () => {
  test("keeps the sidebar fields and ignores the rest", () => {
    expect(decodeIssuePullRequests(JSON.stringify({ pull_requests: [pull], auto_complete: null }))).toEqual([
      {
        id: "pr-1",
        number: 540,
        title: "Fix the scroll",
        state: "closed",
        html_url: "https://github.com/anomalyco/opencode/pull/540",
        repo_owner: "anomalyco",
        repo_name: "opencode",
        branch: "fix-scroll",
        merged_at: "2026-08-01T00:00:00Z",
      },
    ])
  })

  test("treats a missing list as none linked", () => {
    expect(decodeIssuePullRequests("{}")).toEqual([])
  })

  test("rejects a payload that is not the issue pull-request response", () => {
    expect(decodeIssuePullRequests("[]")).toBeUndefined()
    expect(decodeIssuePullRequests("{")).toBeUndefined()
  })
})
