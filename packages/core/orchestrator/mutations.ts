import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "../issues/queries";
import { type OrchFlowAction, type OrchPROp } from "./flow";
import { orchKeys } from "./queries";

export type { OrchFlowAction, OrchPROp } from "./flow";

function useInvalidateOrch(wsId: string) {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: orchKeys.all(wsId) });
    void qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
  };
}

export function useOrchFlowAction(wsId: string, key: string) {
  const invalidate = useInvalidateOrch(wsId);
  return useMutation({
    mutationFn: (v: { action: OrchFlowAction; reason?: string }) =>
      api.orchFlowAction(key, v.action, v.reason ? { reason: v.reason } : undefined),
    onSettled: invalidate,
  });
}

export function useOrchPROp(wsId: string, key: string) {
  const invalidate = useInvalidateOrch(wsId);
  return useMutation({
    mutationFn: (v: OrchPROp | { op: OrchPROp; number?: number; headSha?: string }) =>
      typeof v === "string" ? api.orchPROp(key, v) : api.orchPROp(key, v.op, v.number, v.headSha),
    onSettled: invalidate,
  });
}

// Risk is plain issue metadata (low | medium | high; null clears it).
export function useOrchSetRisk(wsId: string, issueId: string) {
  const invalidate = useInvalidateOrch(wsId);
  return useMutation({
    mutationFn: (risk: "low" | "medium" | "high" | null) => api.setIssueMetadataKey(issueId, "risk", risk),
    onSettled: invalidate,
  });
}

export function useOrchRemoveWorktree(wsId: string) {
  const invalidate = useInvalidateOrch(wsId);
  return useMutation({
    mutationFn: (v: { path: string; force?: boolean }) => api.removeOrchWorktree(v.path, v.force),
    onSettled: invalidate,
  });
}

// Member-editable issue metadata (e.g. acceptance_required); null clears it.
export function useOrchSetMeta(wsId: string, issueId: string) {
  const invalidate = useInvalidateOrch(wsId);
  return useMutation({
    mutationFn: (v: { key: string; value: string | null }) => api.setIssueMetadataKey(issueId, v.key, v.value),
    onSettled: invalidate,
  });
}
