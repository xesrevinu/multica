import { queryOptions } from "@tanstack/react-query";
import { fetchOrchIssues, fetchOrchPRCard, fetchOrchTimeline } from "./api";

export const orchKeys = {
  all: (wsId: string) => ["orch", wsId] as const,
  issues: (wsId: string, settledLimit: number) => [...orchKeys.all(wsId), "issues", settledLimit] as const,
  timeline: (wsId: string, key: string) => [...orchKeys.all(wsId), "timeline", key] as const,
  pr: (wsId: string, key: string) => [...orchKeys.all(wsId), "pr", key] as const,
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

export function orchPRCardOptions(wsId: string, key: string, enabled = true) {
  return queryOptions({
    queryKey: orchKeys.pr(wsId, key),
    queryFn: () => fetchOrchPRCard(key),
    enabled: enabled && !!wsId && !!key,
    staleTime: 30_000,
  });
}
