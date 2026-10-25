import type { OrchAttentionBucket, OrchIssue } from "./schemas";

/** Member-facing flow controls on `/api/orchestrator/flow/:key/:action`. */
export type OrchFlowAction =
  | "accept"
  | "reject"
  | "unblock"
  | "cancel"
  | "pause"
  | "resume"
  | "replay"
  | "resolve-conflict";

/** Member PR controls on `/api/orchestrator/issues/:key/pr/:op`. */
export type OrchPROp = "update-branch" | "rerun-checks" | "merge" | "sync-labels" | "delete-branch";

export type OrchPRAbilities = {
  read_checks: boolean;
  update_branch: boolean;
  rerun: boolean;
  merge: boolean;
  labels: boolean;
  delete_branch: boolean;
};

/**
 * Which PR ops the card should offer — mirrors Web PRCardBody button rules so
 * TUI and Web cannot drift on member-facing controls.
 */
export function availablePROps(
  can: OrchPRAbilities,
  state: {
    live?: boolean;
    open?: boolean;
    orchestrated?: boolean;
    conflict?: boolean;
    failedChecks?: number;
    behind?: number;
    headDeleted?: boolean;
    head?: string;
    base?: string;
  } = {},
): OrchPROp[] {
  if (state.live === false) return [];
  const open = state.open ?? false;
  const orchestrated = state.orchestrated ?? false;
  const conflict = state.conflict ?? false;
  const head = state.head ?? "";
  const base = state.base ?? "";
  const ops: OrchPROp[] = [];
  if (can.labels && !orchestrated) ops.push("sync-labels");
  if (can.delete_branch && !open && !state.headDeleted && head && head !== base) {
    ops.push("delete-branch");
  }
  if (open && can.rerun && (state.failedChecks ?? 0) > 0) ops.push("rerun-checks");
  if (open && !orchestrated && can.update_branch && ((state.behind ?? 0) > 0 || conflict)) {
    ops.push("update-branch");
  }
  if (open && can.merge && !orchestrated) ops.push("merge");
  return ops;
}

/** Flow action offered beside PR ops when an orchestrated PR is conflicted. */
export function shouldOfferResolveConflict(state: {
  live?: boolean;
  open?: boolean;
  orchestrated?: boolean;
  conflict?: boolean;
}): boolean {
  return state.live !== false && Boolean(state.open && state.orchestrated && state.conflict);
}

export type OrchFlowCardMode = "acceptance" | "blocked" | "cancel_proposed" | null;

/** Minimal timeline shape — full OrchTimeline is fine; tests need not invent waiting/*. */
export type OrchFlowCardInput = {
  issue: Pick<OrchIssue, "orchestrated" | "status" | "phase">;
} | null | undefined;

/**
 * Which decision card the issue is waiting on — shared by Web FlowStateCard
 * and the Multica TUI properties panel.
 */
export function flowCardMode(tl: OrchFlowCardInput): OrchFlowCardMode {
  const i = tl?.issue;
  if (!i?.orchestrated || i.status === "done" || i.status === "cancelled") return null;
  if (i.phase === "awaiting_acceptance") return "acceptance";
  if (i.phase === "cancel_proposed") return "cancel_proposed";
  if (i.status === "blocked") return "blocked";
  return null;
}

export function bucketLabel(bucket: OrchAttentionBucket): string {
  switch (bucket) {
    case "needs_you":
      return "needs you";
    case "running":
      return "running";
    case "queued":
      return "queued";
    case "settled":
      return "settled";
  }
}
