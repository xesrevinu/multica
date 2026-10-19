import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { buildChatItems, mergeReviewItems } from "./build-items";

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

  it("slots GitHub reviews and threaded review comments in by time", () => {
    const { items } = buildChatItems([com("c1", 0), com("c2", 10)], []);
    const merged = mergeReviewItems(
      items,
      [{ user: "rev", state: "APPROVED", body: "lgtm", submitted_at: at(5) }, { user: "rev", state: "COMMENTED", body: "", submitted_at: at(6) }],
      [
        { id: 1, user: "rev", path: "a.js", line: 3, body: "nit", created_at: at(7), in_reply_to: 0 },
        { id: 2, user: "dev", path: "a.js", line: 3, body: "fixed", created_at: at(8), in_reply_to: 1 },
      ],
    );
    expect(merged.map((i) => i.id)).toEqual(["c1", `gh-review-0-${at(5)}`, "gh-comment-1", "c2"]);
    const thread = merged[2] as { review: { replies: { body: string }[] } };
    expect(thread.review.replies.map((r) => r.body)).toEqual(["fixed"]);
  });
});
