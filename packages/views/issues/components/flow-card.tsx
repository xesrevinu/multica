"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { OrchestratorFlow } from "@multica/core/api";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

// Rail of the orchestrator state machine (FLOW.md). Blocked and cancelled sit
// off the rail and are shown as a badge on the current node instead.
const RAIL = ["backlog", "todo", "in_progress", "in_review", "acceptance", "done"] as const;

function railIndex(flow: OrchestratorFlow): number {
  if (flow.metadata?.phase === "awaiting_acceptance") return RAIL.indexOf("acceptance");
  const i = RAIL.indexOf(flow.status as (typeof RAIL)[number]);
  return i;
}

export function flowQueryKey(key: string) {
  return ["orchestrator", "flow", key] as const;
}

/**
 * Flow card for issues in an orchestrated project: where the issue is in the
 * state machine, attempt counters, why it is blocked or waiting, what the
 * engine will do next, and the latest transitions. Renders nothing for issues
 * outside orchestrated projects.
 */
export function FlowCard({ identifier }: { identifier: string }) {
  const { t } = useT("issues");
  const qc = useQueryClient();
  const { data: flow } = useQuery({
    queryKey: flowQueryKey(identifier),
    queryFn: () => api.getOrchestratorFlow(identifier),
    refetchInterval: 5000,
    staleTime: 2000,
  });
  const accept = useMutation({
    mutationFn: () => api.acceptOrchestratorFlow(identifier),
    onSettled: () => qc.invalidateQueries({ queryKey: flowQueryKey(identifier) }),
  });
  const [reason, setReason] = useState("");
  const reject = useMutation({
    mutationFn: () => api.rejectOrchestratorFlow(identifier, reason.trim()),
    onSuccess: () => setReason(""),
    onSettled: () => qc.invalidateQueries({ queryKey: flowQueryKey(identifier) }),
  });
  if (!flow) return null;

  const m = flow.metadata ?? {};
  const cur = railIndex(flow);
  const rail = flow.needs_acceptance ? RAIL : RAIL.filter((n) => n !== "acceptance");
  const blockedWhy = flow.status === "blocked" ? m.blocked_reason || flow.last_error : "";
  const next = flow.next;

  return (
    <div className="space-y-2 text-xs" data-testid="orchestrator-flow-card">
      <div className="font-medium text-muted-foreground">{t(($) => $.orchestrator.section)}</div>

      <ol className="flex flex-wrap items-center gap-1">
        {rail.map((node, i) => {
          const idx = RAIL.indexOf(node);
          const isCur = idx === cur;
          return (
            <li key={node} className="flex items-center gap-1">
              {i > 0 && <span className="text-muted-foreground">›</span>}
              <span
                className={cn(
                  "rounded px-1.5 py-0.5",
                  isCur ? "bg-primary text-primary-foreground" : idx < cur ? "bg-muted" : "text-muted-foreground",
                )}
              >
                {node === "acceptance" ? t(($) => $.orchestrator.acceptance) : node.replace("_", " ")}
              </span>
            </li>
          );
        })}
        {(flow.status === "blocked" || flow.status === "cancelled") && (
          <li className="rounded bg-destructive px-1.5 py-0.5 text-destructive-foreground">{flow.status}</li>
        )}
      </ol>

      <div className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-1">
        <span className="text-muted-foreground">{t(($) => $.orchestrator.phase)}</span>
        <span>{m.phase || "-"}</span>
        <span className="text-muted-foreground">{t(($) => $.orchestrator.attempts)}</span>
        <span>
          {t(($) => $.orchestrator.impl)} {m.attempt_impl || 0} · {t(($) => $.orchestrator.review)} {m.attempt_review || 0} ·{" "}
          {t(($) => $.orchestrator.infra)} {m.attempt_infra || 0}
        </span>
        {flow.failures > 0 && (
          <>
            <span className="text-muted-foreground">{t(($) => $.orchestrator.failures)}</span>
            <span className="text-destructive">
              {flow.failures} {flow.last_error}
            </span>
          </>
        )}
        {blockedWhy && (
          <>
            <span className="text-muted-foreground">{t(($) => $.orchestrator.blocked)}</span>
            <span className="text-destructive">{blockedWhy}</span>
          </>
        )}
        {flow.paused && (
          <>
            <span className="text-muted-foreground">{t(($) => $.orchestrator.paused)}</span>
            <span>{flow.paused}</span>
          </>
        )}
        {(flow.waiting || next?.wait) && (
          <>
            <span className="text-muted-foreground">{t(($) => $.orchestrator.waiting)}</span>
            <span>{next?.wait || flow.waiting}</span>
          </>
        )}
        {next?.tid && (
          <>
            <span className="text-muted-foreground">{t(($) => $.orchestrator.next)}</span>
            <span>
              {next.tid}
              {next.why ? `: ${next.why}` : ""}
            </span>
          </>
        )}
      </div>

      {m.phase === "awaiting_acceptance" && (
        <div className="space-y-1" data-testid="orchestrator-acceptance">
          <button
            type="button"
            className="rounded bg-primary px-2 py-1 text-primary-foreground disabled:opacity-50"
            disabled={accept.isPending || reject.isPending}
            onClick={() => accept.mutate()}
          >
            {t(($) => $.orchestrator.accept)}
          </button>
          <div className="flex gap-1">
            <input
              className="min-w-0 flex-1 rounded border bg-background px-1.5 py-0.5"
              value={reason}
              placeholder={t(($) => $.orchestrator.reject_reason)}
              onChange={(e) => setReason(e.target.value)}
            />
            <button
              type="button"
              className="rounded border border-destructive px-2 py-0.5 text-destructive disabled:opacity-50"
              disabled={!reason.trim() || reject.isPending || accept.isPending}
              onClick={() => reject.mutate()}
            >
              {t(($) => $.orchestrator.reject)}
            </button>
          </div>
        </div>
      )}

      {flow.recent && flow.recent.length > 0 && (
        <details>
          <summary className="cursor-pointer text-muted-foreground">{t(($) => $.orchestrator.recent)}</summary>
          <ul className="mt-1 space-y-0.5">
            {flow.recent.map((e) => (
              <li key={e.id} className="flex gap-2">
                <span className="text-muted-foreground tabular-nums">{new Date(e.created_at).toLocaleTimeString()}</span>
                <span className={cn(e.state !== "done" && "text-destructive")}>{e.tid}</span>
                <span className="truncate text-muted-foreground">{e.decision?.why || e.error || ""}</span>
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );
}
