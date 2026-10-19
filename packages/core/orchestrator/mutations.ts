import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "../issues/queries";
import { orchKeys } from "./queries";

export type OrchFlowAction = "accept" | "reject" | "unblock" | "cancel" | "pause" | "resume";
export type OrchPROp = "update-branch" | "rerun-checks" | "merge";

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
    mutationFn: (op: OrchPROp) => api.orchPROp(key, op),
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
