import { describe, expect, it, vi } from "vitest";
import { parseOrchIssues, parseOrchPRCard, parseOrchTimeline } from "./api";
import { availablePROps, flowCardMode, shouldOfferResolveConflict } from "./flow";
import { parseOrchIssueList } from "./parse";
import { DEFAULT_ORCH_VIEW, applyOrchView, filterOrchIssues, groupOrchIssues } from "./tree";
import type { OrchTimeline } from "./schemas";

vi.mock("../api", () => ({ api: {} }));

const row = (over: Record<string, unknown> = {}) => ({
  id: "i1",
  key: "ATT-1",
  number: 1,
  title: "one",
  status: "todo",
  priority: "none",
  labels: [],
  orchestrated: false,
  paused: false,
  attention: { bucket: "queued", since: "2026-10-19T00:00:00Z" },
  children: { total: 0, done: 0 },
  created_at: "",
  updated_at: "",
  ...over,
});

describe("parseOrchIssues", () => {
  it("accepts issues with no project, assignee, metadata, PR or worktree", () => {
    const [i] = parseOrchIssues({ issues: [row()] });
    expect(i?.project).toBeUndefined();
    expect(i?.assignee).toBeUndefined();
    expect(i?.pr).toBeUndefined();
    expect(i?.worktree).toBeUndefined();
  });

  it("drops only malformed rows and degrades bad optional parts", () => {
    const out = parseOrchIssues({
      issues: [row(), { nope: true }, row({ id: "i2", key: "ATT-2", pr: { number: "x" }, attention: { bucket: "weird" }, labels: null })],
    });
    expect(out.map((i) => i.key)).toEqual(["ATT-1", "ATT-2"]);
    expect(out[1]?.pr).toBeUndefined();
    expect(out[1]?.attention.bucket).toBe("queued");
    expect(out[1]?.labels).toEqual([]);
  });

  it("returns [] for a malformed envelope", () => {
    expect(parseOrchIssues(null)).toEqual([]);
    expect(parseOrchIssues({ issues: "x" })).toEqual([]);
  });
});

describe("parseOrchTimeline / parseOrchPRCard", () => {
  it("tolerates missing runs/transitions/flow", () => {
    const t = parseOrchTimeline({ issue: row() });
    expect(t?.runs).toEqual([]);
    expect(t?.transitions).toEqual([]);
    expect(t?.flow).toEqual({});
    expect(parseOrchTimeline({ issue: null })).toBeNull();
  });

  it("defaults PR abilities to false", () => {
    const c = parseOrchPRCard({ mode: "local" });
    expect(c?.mode).toBe("local");
    expect(c?.can).toEqual({ read_checks: false, update_branch: false, rerun: false, merge: false, labels: false, delete_branch: false });
    expect(c?.pr).toBeNull();
  });
});

describe("groupOrchIssues", () => {
  it("nests children, keeps orphans as roots and surfaces needs_you children", () => {
    const issues = parseOrchIssues({
      issues: [
        row({ id: "p", key: "P", attention: { bucket: "running", since: "" } }),
        row({ id: "c1", key: "C1", parent_id: "p" }),
        row({ id: "c2", key: "C2", parent_id: "p", attention: { bucket: "needs_you", since: "" } }),
        row({ id: "o", key: "O", parent_id: "missing" }),
        row({ id: "d", key: "D", attention: { bucket: "settled", since: "" } }),
      ],
    });
    const g = Object.fromEntries(groupOrchIssues(issues).map((x) => [x.bucket, x.nodes.map((n) => n.key)]));
    expect(g).toEqual({ needs_you: ["C2"], running: ["P"], queued: ["O"], settled: ["D"] });
    const p = groupOrchIssues(issues).find((x) => x.bucket === "running")!.nodes[0]!;
    expect(p.subIssues.map((c) => c.key)).toEqual(["C1", "C2"]);
  });

  it("filters by text and project", () => {
    const issues = parseOrchIssues({ issues: [row(), row({ id: "i2", key: "ATT-2", title: "Fix login", project: { id: "pr", title: "web" } })] });
    expect(filterOrchIssues(issues, "login").map((i) => i.key)).toEqual(["ATT-2"]);
    expect(filterOrchIssues(issues, "", "pr").map((i) => i.key)).toEqual(["ATT-2"]);
  });
});

describe("applyOrchView", () => {
  const issues = parseOrchIssues({
    issues: [
      { id: "a", key: "ATT-1", title: "a", priority: "low", orchestrated: true, assignee: { type: "agent", id: "g", name: "dev" }, created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-03T00:00:00Z" },
      { id: "b", key: "ATT-2", title: "b", priority: "urgent", assignee: { type: "member", id: "u1", name: "Ray" }, created_at: "2026-10-02T00:00:00Z", updated_at: "2026-10-02T00:00:00Z" },
      { id: "c", key: "ATT-3", title: "c", created_at: "2026-10-03T00:00:00Z", updated_at: "2026-10-01T00:00:00Z" },
    ],
  });
  it("keeps server order for attention and tolerates missing assignee/priority", () => {
    expect(applyOrchView(issues, DEFAULT_ORCH_VIEW).map((i) => i.key)).toEqual(["ATT-1", "ATT-2", "ATT-3"]);
  });
  it("sorts and filters", () => {
    expect(applyOrchView(issues, { ...DEFAULT_ORCH_VIEW, sort: "priority" }).map((i) => i.key)).toEqual(["ATT-2", "ATT-1", "ATT-3"]);
    expect(applyOrchView(issues, { ...DEFAULT_ORCH_VIEW, sort: "created" }).map((i) => i.key)).toEqual(["ATT-3", "ATT-2", "ATT-1"]);
    expect(applyOrchView(issues, { ...DEFAULT_ORCH_VIEW, assignee: "me" }, "u1").map((i) => i.key)).toEqual(["ATT-2"]);
    expect(applyOrchView(issues, { ...DEFAULT_ORCH_VIEW, assignee: "unassigned" }).map((i) => i.key)).toEqual(["ATT-3"]);
    expect(applyOrchView(issues, { ...DEFAULT_ORCH_VIEW, orchestratedOnly: true }).map((i) => i.key)).toEqual(["ATT-1"]);
    expect(applyOrchView(issues, { ...DEFAULT_ORCH_VIEW, priorities: ["none"] }).map((i) => i.key)).toEqual(["ATT-3"]);
  });
});


describe("flowCardMode", () => {
  const base = (partial: Partial<Pick<OrchTimeline["issue"], "phase" | "status" | "orchestrated">> = {}) => ({
    issue: {
      orchestrated: true as const,
      status: "in_review",
      phase: "awaiting_acceptance" as string | undefined,
      ...partial,
    },
  });

  it("classifies acceptance / blocked / cancel / done", () => {
    expect(flowCardMode(base())).toBe("acceptance");
    expect(flowCardMode(base({ phase: "cancel_proposed" }))).toBe("cancel_proposed");
    expect(flowCardMode(base({ phase: undefined, status: "blocked" }))).toBe("blocked");
    expect(flowCardMode(base({ status: "done" }))).toBeNull();
  });
});

describe("parseOrchIssueList", () => {
  it("keeps settled_total and drops bad rows", () => {
    const list = parseOrchIssueList({
      settled_total: 4,
      issues: [
        {
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
        { broken: true },
      ],
    });
    expect(list.issues).toHaveLength(1);
    expect(list.settledTotal).toBe(4);
  });
});


describe("availablePROps", () => {
  const can = {
    read_checks: true,
    update_branch: true,
    rerun: true,
    merge: true,
    labels: true,
    delete_branch: true,
  };

  it("hides member merge/update when orchestrated", () => {
    expect(
      availablePROps(can, {
        live: true,
        open: true,
        orchestrated: true,
        failedChecks: 2,
        behind: 3,
      }),
    ).toEqual(["rerun-checks"]);
  });

  it("offers merge/update for non-orchestrated open PRs", () => {
    expect(
      availablePROps(can, {
        live: true,
        open: true,
        orchestrated: false,
        behind: 2,
        conflict: false,
      }),
    ).toEqual(["sync-labels", "update-branch", "merge"]);
  });

  it("offers delete-branch only when closed with a head", () => {
    expect(
      availablePROps(can, {
        live: true,
        open: false,
        head: "agent/ATT-1",
        base: "main",
      }),
    ).toEqual(["sync-labels", "delete-branch"]);
  });

  it("shouldOfferResolveConflict follows Web rules", () => {
    expect(shouldOfferResolveConflict({ live: true, open: true, orchestrated: true, conflict: true })).toBe(true);
    expect(shouldOfferResolveConflict({ live: true, open: true, orchestrated: false, conflict: true })).toBe(false);
  });
});
