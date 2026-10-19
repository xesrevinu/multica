import type { OrchAttentionBucket, OrchIssue } from "./schemas";

export type OrchIssueNode = OrchIssue & { subIssues: OrchIssueNode[] };

export type OrchIssueGroup = { bucket: OrchAttentionBucket; nodes: OrchIssueNode[]; count: number };

export const ORCH_BUCKETS: OrchAttentionBucket[] = ["needs_you", "running", "queued", "settled"];

// groupOrchIssues nests sub-issues under their parent when the parent is in
// the list (keeping server order) and groups roots by attention bucket. A
// child whose parent is missing (filtered/settled out) stays a root. A child
// that needs the user also surfaces as a root in needs_you so it is never
// hidden under a collapsed parent.
export function groupOrchIssues(issues: OrchIssue[]): OrchIssueGroup[] {
  const nodes = new Map<string, OrchIssueNode>();
  for (const i of issues) nodes.set(i.id, { ...i, subIssues: [] });
  const roots: OrchIssueNode[] = [];
  for (const i of issues) {
    const n = nodes.get(i.id)!;
    const parent = i.parent_id ? nodes.get(i.parent_id) : undefined;
    if (parent && parent.id !== n.id) {
      parent.subIssues.push(n);
      if (n.attention.bucket === "needs_you" && parent.attention.bucket !== "needs_you") roots.push(n);
    } else {
      roots.push(n);
    }
  }
  return ORCH_BUCKETS.map((bucket) => {
    const list = roots.filter((n) => n.attention.bucket === bucket);
    return { bucket, nodes: list, count: list.length };
  });
}

export function filterOrchIssues(issues: OrchIssue[], query: string, projectId?: string | null): OrchIssue[] {
  const q = query.trim().toLowerCase();
  return issues.filter((i) => {
    if (projectId && i.project?.id !== projectId) return false;
    if (!q) return true;
    return i.key.toLowerCase().includes(q) || i.title.toLowerCase().includes(q);
  });
}

// --- sidebar view options ------------------------------------------------------

export type OrchSort = "attention" | "updated" | "created" | "priority";
export type OrchAssigneeFilter = "any" | "me" | "agents" | "unassigned";

export type OrchViewOptions = {
  sort: OrchSort;
  assignee: OrchAssigneeFilter;
  priorities: string[]; // empty = any
  orchestratedOnly: boolean;
  showSubIssues: boolean;
};

export const DEFAULT_ORCH_VIEW: OrchViewOptions = {
  sort: "attention",
  assignee: "any",
  priorities: [],
  orchestratedOnly: false,
  showSubIssues: true,
};

const PRIORITY_RANK: Record<string, number> = { urgent: 0, high: 1, medium: 2, low: 3, none: 4 };

const time = (s: string) => {
  const t = new Date(s).getTime();
  return Number.isFinite(t) ? t : 0;
};

// applyOrchView filters by assignee/priority/orchestration and re-sorts within
// the server's attention order. "attention" keeps the server order (needs_you
// oldest first, then priority, then recency).
export function applyOrchView(issues: OrchIssue[], v: OrchViewOptions, userId?: string): OrchIssue[] {
  let out = issues.filter((i) => {
    if (v.orchestratedOnly && !i.orchestrated) return false;
    if (v.priorities.length > 0 && !v.priorities.includes(i.priority || "none")) return false;
    switch (v.assignee) {
      case "me":
        return i.assignee?.type === "member" && !!userId && i.assignee.id === userId;
      case "agents":
        return i.assignee?.type === "agent";
      case "unassigned":
        return !i.assignee;
      default:
        return true;
    }
  });
  if (v.sort !== "attention") {
    const cmp: Record<Exclude<OrchSort, "attention">, (a: OrchIssue, b: OrchIssue) => number> = {
      updated: (a, b) => time(b.updated_at) - time(a.updated_at),
      created: (a, b) => time(b.created_at) - time(a.created_at),
      priority: (a, b) => (PRIORITY_RANK[a.priority] ?? 4) - (PRIORITY_RANK[b.priority] ?? 4) || time(b.updated_at) - time(a.updated_at),
    };
    out = [...out].sort(cmp[v.sort]);
  }
  return out;
}

// diffOf is the PR line delta for list chips (0/0 when there is no PR).
export function diffOf(i: Pick<OrchIssue, "pr">): { additions: number; deletions: number } {
  return { additions: i.pr?.additions ?? 0, deletions: i.pr?.deletions ?? 0 };
}
