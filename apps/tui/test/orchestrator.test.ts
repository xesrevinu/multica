import { describe, expect, test } from "bun:test"
import {
  bucketLabel,
  flowCardMode,
  groupOrchIssues,
  parseOrchIssueList,
  parseOrchTimeline,
  type OrchIssue,
} from "../src/orchestrator"

function issue(
  partial: {
    id: string
    key: string
    title?: string
    status?: string
    priority?: string
    parent_id?: string
    phase?: string
    attention?: Partial<OrchIssue["attention"]>
  },
): OrchIssue {
  return {
    id: partial.id,
    key: partial.key,
    number: 1,
    title: partial.title ?? partial.key,
    status: partial.status ?? "todo",
    priority: partial.priority ?? "none",
    project: undefined,
    parent_id: partial.parent_id,
    assignee: undefined,
    labels: [],
    orchestrated: true,
    mode: undefined,
    phase: partial.phase,
    risk: undefined,
    expert: undefined,
    paused: false,
    attempts: undefined,
    attention: {
      bucket: partial.attention?.bucket ?? "queued",
      reason: partial.attention?.reason,
      since: partial.attention?.since ?? "2026-10-01T00:00:00Z",
    },
    running: undefined,
    pr: undefined,
    worktree: undefined,
    children: { total: 0, done: 0 },
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-02T00:00:00Z",
  }
}

describe("orchestrator shared façade", () => {
  test("re-exports core parsers and buckets", () => {
    const { issues, settledTotal } = parseOrchIssueList({
      settled_total: 2,
      issues: [
        {
          id: "1",
          key: "ATT-1",
          number: 1,
          title: "One",
          status: "blocked",
          priority: "high",
          labels: [],
          orchestrated: true,
          paused: false,
          attention: { bucket: "needs_you", reason: "blocked", since: "2026-10-01T00:00:00Z" },
          children: { total: 0, done: 0 },
          created_at: "2026-10-01T00:00:00Z",
          updated_at: "2026-10-02T00:00:00Z",
        },
      ],
    })
    expect(issues).toHaveLength(1)
    expect(issues[0]?.attention.bucket).toBe("needs_you")
    expect(settledTotal).toBe(2)
    expect(bucketLabel("needs_you")).toBe("needs you")
  })

  test("groups nested children with shared tree helper", () => {
    const parent = issue({ id: "p", key: "ATT-1", attention: { bucket: "running" } })
    const child = issue({
      id: "c",
      key: "ATT-2",
      parent_id: "p",
      attention: { bucket: "needs_you", reason: "acceptance", since: "2026-10-02T00:00:00Z" },
    })
    const groups = groupOrchIssues([parent, child])
    const needs = groups.find((g) => g.bucket === "needs_you")
    expect(needs?.nodes.map((n) => n.key)).toContain("ATT-2")
    const running = groups.find((g) => g.bucket === "running")
    expect(running?.nodes[0]?.subIssues.map((n) => n.key)).toEqual(["ATT-2"])
  })

  test("flowCardMode matches Web", () => {
    const base = {
      issue: {
        orchestrated: true,
        phase: "awaiting_acceptance" as string | undefined,
        status: "in_review",
      },
    }
    expect(flowCardMode(base)).toBe("acceptance")
    expect(flowCardMode({ issue: { ...base.issue, phase: "cancel_proposed" } })).toBe("cancel_proposed")
    expect(flowCardMode({ issue: { ...base.issue, phase: undefined, status: "blocked" } })).toBe("blocked")
    expect(flowCardMode({ issue: { ...base.issue, status: "done" } })).toBeNull()
  })

  test("timeline parse tolerates sparse payloads", () => {
    const tl = parseOrchTimeline({
      issue: {
        id: "1",
        key: "ATT-1",
        number: 1,
        title: "One",
        status: "todo",
        priority: "none",
        labels: [],
        orchestrated: true,
        paused: false,
        attention: { bucket: "queued", since: "2026-10-01T00:00:00Z" },
        children: { total: 0, done: 0 },
        created_at: "2026-10-01T00:00:00Z",
        updated_at: "2026-10-01T00:00:00Z",
      },
    })
    expect(tl?.issue.key).toBe("ATT-1")
    expect(tl?.flow).toEqual({})
  })
})
