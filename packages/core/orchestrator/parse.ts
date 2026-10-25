import { parseWithFallback } from "../api/schema";
import {
  OrchIssueSchema,
  OrchLinkedPRSchema,
  OrchPRCardSchema,
  OrchPRFilesSchema,
  OrchTimelineSchema,
  OrchWorktreesSchema,
  type OrchIssue,
  type OrchLinkedPR,
  type OrchPRCard,
  type OrchPRFiles,
  type OrchTimeline,
  type OrchWorktrees,
} from "./schemas";

export type OrchIssueList = { issues: OrchIssue[]; settledTotal: number };

/** Settled total from the list payload, falling back to counting settled rows. */
export function orchSettledTotal(raw: unknown, issues: readonly OrchIssue[]): number {
  const total = (raw as { settled_total?: unknown } | null)?.settled_total;
  if (typeof total === "number") return total;
  return issues.filter((i) => i.attention.bucket === "settled").length;
}

// Rows are parsed one by one so a single malformed issue is dropped instead
// of blanking the whole sidebar.
export function parseOrchIssues(raw: unknown): OrchIssue[] {
  const rows = (raw as { issues?: unknown } | null)?.issues;
  if (!Array.isArray(rows)) {
    parseWithFallback(raw, OrchIssueSchema.array(), [], { endpoint: "GET /api/orchestrator/issues" });
    return [];
  }
  const out: OrchIssue[] = [];
  for (const row of rows) {
    const v = parseWithFallback<OrchIssue | null>(row, OrchIssueSchema, null, {
      endpoint: "GET /api/orchestrator/issues[row]",
    });
    if (v) out.push(v);
  }
  return out;
}

export function parseOrchIssueList(raw: unknown): OrchIssueList {
  const issues = parseOrchIssues(raw);
  return { issues, settledTotal: orchSettledTotal(raw, issues) };
}

export function parseOrchTimeline(raw: unknown): OrchTimeline | null {
  return parseWithFallback<OrchTimeline | null>(raw, OrchTimelineSchema, null, {
    endpoint: "GET /api/orchestrator/issues/:key/timeline",
  });
}

export function parseOrchPRCard(raw: unknown): OrchPRCard | null {
  return parseWithFallback<OrchPRCard | null>(raw, OrchPRCardSchema, null, {
    endpoint: "GET /api/orchestrator/issues/:key/pr",
  });
}

export function parseOrchPRList(raw: unknown): OrchLinkedPR[] {
  const rows = (raw as { prs?: unknown } | null)?.prs ?? [];
  return parseWithFallback<OrchLinkedPR[]>(rows, OrchLinkedPRSchema.array(), [], {
    endpoint: "GET /api/orchestrator/issues/:key/prs",
  });
}

export function parseOrchPRFiles(raw: unknown): OrchPRFiles | null {
  return parseWithFallback<OrchPRFiles | null>(raw, OrchPRFilesSchema, null, {
    endpoint: "GET /api/orchestrator/issues/:key/pr/files",
  });
}

export function parseOrchWorktrees(raw: unknown): OrchWorktrees {
  return parseWithFallback<OrchWorktrees>(raw, OrchWorktreesSchema, { hubs: [], worktrees: [] }, {
    endpoint: "GET /api/orchestrator/worktrees",
  });
}
