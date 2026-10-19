import type { TimelineEntry } from "@multica/core/types";
import { orderTimelineWithRuns, type CommentRun } from "../../issues/components/comment-runs";
import { collectThreadReplies } from "../../issues/components/thread-utils";

// Conversation items, oldest first (the view renders newest at the bottom).
export type ChatItem =
  | { kind: "comment"; id: string; entry: TimelineEntry }
  | { kind: "run"; id: string; run: CommentRun; reply?: TimelineEntry }
  | { kind: "transition"; id: string; entry: TimelineEntry }
  | { kind: "activities"; id: string; entries: TimelineEntry[] };

export type ChatModel = {
  items: ChatItem[];
  threadReplies: Map<string, TimelineEntry[]>;
};

// buildChatItems turns the issue timeline (+ runs) into conversation items:
// root comments with their replies threaded, runs in time order,
// orchestrator transitions as dividers, and runs of other activities folded.
export function buildChatItems(timeline: readonly TimelineEntry[], standaloneRuns: readonly CommentRun[]): ChatModel {
  const topLevel = timeline.filter((e) => e.type === "activity" || !e.parent_id);
  const repliesByParent = new Map<string, TimelineEntry[]>();
  for (const e of timeline) {
    if (e.type === "comment" && e.parent_id) {
      const list = repliesByParent.get(e.parent_id) ?? [];
      list.push(e);
      repliesByParent.set(e.parent_id, list);
    }
  }
  const threadReplies = new Map<string, TimelineEntry[]>();
  for (const root of topLevel) {
    if (root.type === "comment") threadReplies.set(root.id, collectThreadReplies(root.id, repliesByParent));
  }
  const entryById = new Map(timeline.map((e) => [e.id, e]));
  const ordered = orderTimelineWithRuns(topLevel, [...standaloneRuns], entryById);
  const items: ChatItem[] = [];
  for (const x of ordered) {
    if ("task" in x) {
      const reply = x.hasReply && x.commentId ? entryById.get(x.commentId) : undefined;
      items.push({ kind: "run", id: reply?.id ?? x.task.id, run: x, reply });
      continue;
    }
    if (x.type === "comment") {
      items.push({ kind: "comment", id: x.id, entry: x });
      continue;
    }
    if (x.action === "orchestrator_transition") {
      items.push({ kind: "transition", id: x.id, entry: x });
      continue;
    }
    const last = items[items.length - 1];
    if (last?.kind === "activities") last.entries.push(x);
    else items.push({ kind: "activities", id: x.id, entries: [x] });
  }
  return { items, threadReplies };
}

// Duration of a run: start (or enqueue) to end (or now).
export function runDurationMs(run: CommentRun, now = Date.now()): number {
  const t = run.task as { created_at?: string; started_at?: string | null; completed_at?: string | null };
  const start = new Date(t.started_at ?? t.created_at ?? now).getTime();
  const end = t.completed_at ? new Date(t.completed_at).getTime() : now;
  return Number.isFinite(start) && Number.isFinite(end) ? Math.max(0, end - start) : 0;
}
