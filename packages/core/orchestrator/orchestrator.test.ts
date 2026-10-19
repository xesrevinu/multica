import { describe, expect, it, vi } from "vitest";
import { parseOrchIssues, parseOrchPRCard, parseOrchTimeline } from "./api";
import { filterOrchIssues, groupOrchIssues } from "./tree";

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
    expect(c?.can).toEqual({ read_checks: false, update_branch: false, rerun: false, merge: false });
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
