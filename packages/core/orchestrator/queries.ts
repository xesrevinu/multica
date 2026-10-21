import { queryOptions } from "@tanstack/react-query";
import { fetchOrchIssues, fetchOrchPRCard, fetchOrchPRFiles, fetchOrchPRList, fetchOrchTimeline, fetchOrchWorktrees } from "./api";

export const orchKeys = {
  all: (wsId: string) => ["orch", wsId] as const,
  issues: (wsId: string, settledLimit: number) => [...orchKeys.all(wsId), "issues", settledLimit] as const,
  timeline: (wsId: string, key: string) => [...orchKeys.all(wsId), "timeline", key] as const,
  pr: (wsId: string, key: string, number?: number) => [...orchKeys.all(wsId), "pr", key, number ?? 0] as const,
  prs: (wsId: string, key: string) => [...orchKeys.all(wsId), "prs", key] as const,
  files: (wsId: string, key: string, number?: number) => [...orchKeys.all(wsId), "files", key, number ?? 0] as const,
  worktrees: (wsId: string, issueKey?: string) => [...orchKeys.all(wsId), "worktrees", issueKey ?? ""] as const,
};

export function orchIssuesOptions(wsId: string, settledLimit = 30) {
  return queryOptions({
    queryKey: orchKeys.issues(wsId, settledLimit),
    queryFn: () => fetchOrchIssues({ settledLimit }),
    enabled: !!wsId,
    staleTime: 10_000,
  });
}

export function orchTimelineOptions(wsId: string, key: string) {
  return queryOptions({
    queryKey: orchKeys.timeline(wsId, key),
    queryFn: () => fetchOrchTimeline(key),
    enabled: !!wsId && !!key,
    staleTime: 5_000,
  });
}

export function orchPRCardOptions(wsId: string, key: string, enabled = true, number?: number) {
  return queryOptions({
    queryKey: orchKeys.pr(wsId, key, number),
    queryFn: () => fetchOrchPRCard(key, number),
    enabled: enabled && !!wsId && !!key,
    staleTime: 30_000,
  });
}

export function orchPRListOptions(wsId: string, key: string, enabled = true) {
  return queryOptions({
    queryKey: orchKeys.prs(wsId, key),
    queryFn: () => fetchOrchPRList(key),
    enabled: enabled && !!wsId && !!key,
    staleTime: 30_000,
  });
}

export function orchPRFilesOptions(wsId: string, key: string, enabled = true, number?: number) {
  return queryOptions({
    queryKey: orchKeys.files(wsId, key, number),
    queryFn: () => fetchOrchPRFiles(key, number),
    enabled: enabled && !!wsId && !!key,
    staleTime: 60_000,
  });
}

export function orchWorktreesOptions(wsId: string, issueKey?: string) {
  return queryOptions({
    queryKey: orchKeys.worktrees(wsId, issueKey),
    queryFn: () => fetchOrchWorktrees(issueKey),
    enabled: !!wsId,
    staleTime: 15_000,
  });
}
