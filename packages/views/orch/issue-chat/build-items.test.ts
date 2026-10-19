import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { buildChatItems } from "./build-items";

const at = (m: number) => new Date(Date.UTC(2026, 9, 19, 10, m)).toISOString();
const act = (id: string, m: number, action: string): TimelineEntry => ({ type: "activity", id, actor_type: "member", actor_id: "u", created_at: at(m), action, details: {} });
const com = (id: string, m: number, parent?: string): TimelineEntry => ({ type: "comment", id, actor_type: "member", actor_id: "u", created_at: at(m), content: id, parent_id: parent ?? null });

describe("buildChatItems", () => {
  it("threads replies, keeps transitions as dividers and folds other activities", () => {
    const { items, threadReplies } = buildChatItems(
      [act("a1", 0, "created"), act("a2", 1, "status_changed"), act("t1", 2, "orchestrator_transition"), com("c1", 3), com("r1", 4, "c1"), act("a3", 5, "priority_changed")],
      [],
    );
    expect(items.map((i) => `${i.kind}:${i.id}`)).toEqual(["activities:a1", "transition:t1", "comment:c1", "activities:a3"]);
    expect((items[0] as { entries: TimelineEntry[] }).entries.map((e) => e.id)).toEqual(["a1", "a2"]);
    expect(threadReplies.get("c1")?.map((e) => e.id)).toEqual(["r1"]);
  });

  it("handles an empty timeline", () => {
    expect(buildChatItems([], []).items).toEqual([]);
  });
});
