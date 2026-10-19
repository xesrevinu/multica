import type { TimelineEntry } from "@multica/core/types";
import { orderTimelineWithRuns, type CommentRun } from "../../issues/components/comment-runs";
import { collectThreadReplies } from "../../issues/components/thread-utils";

// Conversation items, oldest first (the view renders newest at the bottom).
export type ChatItem =
  | { kind: "comment"; id: string; entry: TimelineEntry }
  | { kind: "run"; id: string; run: CommentRun; reply?: TimelineEntry }
  | { kind: "transition"; id: string; entry: TimelineEntry }
  | { kind: "activities"; id: string; entries: TimelineEntry[] }
  | { kind: "review"; id: string; review: GitHubReviewItem };

// A GitHub PR review or review comment surfaced in the conversation.
export type GitHubReviewItem = {
  type: "review" | "comment";
  user: string;
  avatar?: string;
  state?: string;
  body: string;
  path?: string;
  line?: number;
  url?: string;
  at: string;
  replies?: GitHubReviewItem[];
};

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

function itemTime(i: ChatItem): number {
  const iso =
    i.kind === "review"
      ? i.review.at
      : i.kind === "run"
        ? (i.reply?.created_at ?? i.run.task.created_at)
        : i.kind === "activities"
          ? i.entries[0]?.created_at
          : i.entry.created_at;
  const t = iso ? new Date(iso).getTime() : NaN;
  return Number.isFinite(t) ? t : 0;
}

// mergeReviewItems slots GitHub reviews / review comments into the
// conversation by time. Review comments are threaded by in_reply_to; empty
// review bodies (inline-only reviews) are dropped since their comments show.
export function mergeReviewItems(
  items: ChatItem[],
  reviews: readonly { user: string; avatar?: string; state: string; body: string; submitted_at: string; url?: string }[],
  comments: readonly { id: number; user: string; avatar?: string; path: string; line: number; body: string; created_at: string; url?: string; in_reply_to: number }[],
): ChatItem[] {
  const extra: ChatItem[] = [];
  reviews.forEach((r, n) => {
    if (!r.body.trim() || !r.submitted_at) return;
    extra.push({ kind: "review", id: `gh-review-${n}-${r.submitted_at}`, review: { type: "review", user: r.user, avatar: r.avatar, state: r.state, body: r.body, url: r.url, at: r.submitted_at } });
  });
  const roots = new Map<number, GitHubReviewItem>();
  for (const c of [...comments].sort((a, b) => a.created_at.localeCompare(b.created_at))) {
    const item: GitHubReviewItem = { type: "comment", user: c.user, avatar: c.avatar, body: c.body, path: c.path, line: c.line || undefined, url: c.url, at: c.created_at, replies: [] };
    const root = c.in_reply_to ? roots.get(c.in_reply_to) : undefined;
    if (root) {
      root.replies!.push(item);
      roots.set(c.id, root);
      continue;
    }
    roots.set(c.id, item);
    extra.push({ kind: "review", id: `gh-comment-${c.id}`, review: item });
  }
  if (extra.length === 0) return items;
  return [...items, ...extra]
    .map((it, idx) => ({ it, idx, t: itemTime(it) }))
    .sort((a, b) => a.t - b.t || a.idx - b.idx)
    .map((x) => x.it);
}
