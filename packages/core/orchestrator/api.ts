import { api } from "../api";
import {
  parseOrchIssueList,
  parseOrchPRCard,
  parseOrchPRFiles,
  parseOrchPRList,
  parseOrchTimeline,
  parseOrchWorktrees,
  type OrchIssueList,
} from "./parse";
import type { OrchLinkedPR, OrchPRCard, OrchPRFiles, OrchTimeline, OrchWorktrees } from "./schemas";

export {
  orchSettledTotal,
  parseOrchIssueList,
  parseOrchIssues,
  parseOrchPRCard,
  parseOrchPRFiles,
  parseOrchPRList,
  parseOrchTimeline,
  parseOrchWorktrees,
  type OrchIssueList,
} from "./parse";

export async function fetchOrchIssues(params?: { settledLimit?: number; project?: string }): Promise<OrchIssueList> {
  return parseOrchIssueList(await api.listOrchIssues(params));
}

export async function fetchOrchTimeline(key: string): Promise<OrchTimeline | null> {
  return parseOrchTimeline(await api.getOrchIssueTimeline(key));
}

export async function fetchOrchPRCard(key: string, number?: number): Promise<OrchPRCard | null> {
  return parseOrchPRCard(await api.getOrchIssuePR(key, number));
}

export async function fetchOrchPRList(key: string): Promise<OrchLinkedPR[]> {
  return parseOrchPRList(await api.listOrchIssuePRs(key));
}

export async function fetchOrchPRFiles(key: string, number?: number): Promise<OrchPRFiles | null> {
  return parseOrchPRFiles(await api.getOrchIssuePRFiles(key, number));
}

export async function fetchOrchWorktrees(issueKey?: string): Promise<OrchWorktrees> {
  return parseOrchWorktrees(await api.listOrchWorktrees(issueKey));
}
