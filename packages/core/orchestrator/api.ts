import { api } from "../api";
import { parseWithFallback } from "../api/schema";
import {
  OrchLinkedPRSchema,
  OrchPRFilesSchema,
  OrchWorktreesSchema,
  type OrchLinkedPR,
  type OrchPRFiles,
  type OrchWorktrees,
  OrchIssueSchema,
  OrchPRCardSchema,
  OrchTimelineSchema,
  type OrchIssue,
  type OrchPRCard,
  type OrchTimeline,
} from "./schemas";

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
    const v = parseWithFallback<OrchIssue | null>(row, OrchIssueSchema, null, { endpoint: "GET /api/orchestrator/issues[row]" });
    if (v) out.push(v);
  }
  return out;
}

export function parseOrchTimeline(raw: unknown): OrchTimeline | null {
  return parseWithFallback<OrchTimeline | null>(raw, OrchTimelineSchema, null, { endpoint: "GET /api/orchestrator/issues/:key/timeline" });
}

export function parseOrchPRCard(raw: unknown): OrchPRCard | null {
  return parseWithFallback<OrchPRCard | null>(raw, OrchPRCardSchema, null, { endpoint: "GET /api/orchestrator/issues/:key/pr" });
}

export async function fetchOrchIssues(params?: { settledLimit?: number; project?: string }) {
  return parseOrchIssues(await api.listOrchIssues(params));
}

export async function fetchOrchTimeline(key: string) {
  return parseOrchTimeline(await api.getOrchIssueTimeline(key));
}

export async function fetchOrchPRCard(key: string, number?: number) {
  return parseOrchPRCard(await api.getOrchIssuePR(key, number));
}

export async function fetchOrchPRList(key: string): Promise<OrchLinkedPR[]> {
  const raw = (await api.listOrchIssuePRs(key)) as { prs?: unknown } | null;
  return parseWithFallback<OrchLinkedPR[]>(raw?.prs ?? [], OrchLinkedPRSchema.array(), [], { endpoint: "GET /api/orchestrator/issues/:key/prs" });
}

export async function fetchOrchPRFiles(key: string, number?: number): Promise<OrchPRFiles | null> {
  return parseWithFallback<OrchPRFiles | null>(await api.getOrchIssuePRFiles(key, number), OrchPRFilesSchema, null, {
    endpoint: "GET /api/orchestrator/issues/:key/pr/files",
  });
}

export async function fetchOrchWorktrees(issueKey?: string): Promise<OrchWorktrees> {
  return parseWithFallback<OrchWorktrees>(await api.listOrchWorktrees(issueKey), OrchWorktreesSchema, { hubs: [], worktrees: [] }, {
    endpoint: "GET /api/orchestrator/worktrees",
  });
}
