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
