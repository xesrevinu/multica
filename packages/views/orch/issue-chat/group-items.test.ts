import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { dedupeEvents, failureSignature, groupChatItems, type ChatItem } from "./build-items";

const run = (id: string, agent: string, error: string, status = "failed"): ChatItem => ({
  kind: "run",
  id,
  run: { hasReply: false, task: { id, agent_id: agent, status, error, failure_reason: "environment_prepare_failed", created_at: "2026-10-22T12:57:00Z" } as never },
});
const tr = (id: string, at: string, details: Record<string, string>): ChatItem => ({
  kind: "transition",
  id,
  entry: { id, type: "activity", action: "orchestrator_transition", created_at: at, details } as unknown as TimelineEntry,
});
const act = (id: string, at: string, action: string): ChatItem => ({
  kind: "activities",
  id,
  entries: [{ id, type: "activity", action, created_at: at, details: {} } as unknown as TimelineEntry],
});
const occ = (b: string, id: string) => `prepare execution environment: [branch_occupied] orchestrator branch agent/${b} is held (task ${id})`;

describe("groupChatItems", () => {
  it("folds same-error failures across agents and the churn between them", () => {
    const out = groupChatItems([
      run("a", "reviewer", occ("ox-180", "01a163b0-8f00-77e3-b2b1-2ff9b038eb27")),
      tr("t1", "2026-10-22T12:57:55Z", { tid: "T3b_rereview" }),
      act("x1", "2026-10-22T12:57:56Z", "status_changed"),
      run("b", "lead", occ("ox-180", "01a163b1-0b3c-781a-9ae2-5e430c559ee0")),
      run("c", "engineer", occ("ox-180", "01a163b4-5b94-7bc7-9ab1-524608fcaac6")),
      tr("t2", "2026-10-22T12:58:00Z", { tid: "T6_block", status: "blocked" }),
      run("d", "engineer", "something else entirely"),
    ]);
    expect(out.map((i) => i.kind)).toEqual(["failures", "events", "run"]);
    const g = out[0] as Extract<ChatItem, { kind: "failures" }>;
    expect(g.runs.map((r) => r.task.id)).toEqual(["a", "b", "c"]);
    expect(g.between.map((e) => e.id)).toEqual(["t1", "x1"]);
  });

  it("leaves a single failure alone and merges transition + activity bursts", () => {
    const out = groupChatItems([run("a", "r", "boom"), tr("t1", "2026-10-22T12:00:00Z", { tid: "T2_review" }), act("x", "2026-10-22T12:00:01Z", "assignee_changed")]);
    expect(out.map((i) => i.kind)).toEqual(["run", "events"]);
    expect((out[1] as Extract<ChatItem, { kind: "events" }>).entries).toHaveLength(2);
  });

  it("signature ignores ids and numbers but not the reason", () => {
    const a = { error: "x 01a163b0-8f00-77e3-b2b1-2ff9b038eb27 cfb76d35 12", failure_reason: "r" } as never;
    const b = { error: "x 01a163b4-9ebe-7695-b9a7-2e3a31e37480 70e71bf9 99", failure_reason: "r" } as never;
    expect(failureSignature(a)).toBe(failureSignature(b));
    expect(failureSignature({ ...(a as object), failure_reason: "q" } as never)).not.toBe(failureSignature(a));
  });
});

describe("dedupeEvents", () => {
  it("drops the status/assignee activities the engine wrote with its transition", () => {
    const t1 = (tr("t", "2026-10-22T12:00:00Z", { tid: "T2_review", status: "in_review", assignee: "rev" }) as { entry: TimelineEntry }).entry;
    const s = (act("s", "2026-10-22T12:00:02Z", "status_changed") as { entries: TimelineEntry[] }).entries[0]!;
    const late = (act("l", "2026-10-22T12:10:00Z", "status_changed") as { entries: TimelineEntry[] }).entries[0]!;
    expect(dedupeEvents([t1, s, late]).map((e) => e.id)).toEqual(["t", "l"]);
  });
});
