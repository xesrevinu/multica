"use client";

import { toast } from "sonner";
import { Ban, Copy, FolderGit2, MoreHorizontal, Pause, Play, ShieldAlert } from "lucide-react";
import type { Issue, IssuePriority } from "@multica/core/types";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { useOrchSetRisk, type OrchTimeline } from "@multica/core/orchestrator";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { formatDuration, since } from "../format";
import { usePhaseLabel } from "../labels";
import { useFlowAction } from "./flow-cards";
import { useStatusText } from "./timeline-parts";

const PRIORITIES: IssuePriority[] = ["urgent", "high", "medium", "low", "none"];
const RISKS = ["low", "medium", "high"] as const;

// StatusStrip: one quiet line under the composer — where the issue is in the
// flow, who is working, the worktree, and the fixed member actions
// (priority, risk, pause/resume, cancel). No free reassignment here.
export function StatusStrip({ wsId, issue, timeline }: { wsId: string; issue: Issue; timeline: OrchTimeline | null | undefined }) {
  const { t } = useT("orch");
  const { t: ti } = useT("issues");
  const phaseLabel = usePhaseLabel();
  const statusText = useStatusText();
  const v = timeline?.issue;
  const key = issue.identifier;
  const { run, pending } = useFlowAction(wsId, key);
  const updateIssue = useUpdateIssue();
  const setRisk = useOrchSetRisk(wsId, issue.id);
  const settled = issue.status === "done" || issue.status === "cancelled";
  const orchestrated = !!v?.orchestrated;
  const wt = v?.worktree;
  const copy = (text: string) => {
    void navigator.clipboard?.writeText(text).then(() => toast.success(t(($) => $.chat.copied)));
  };
  return (
    <div className="flex min-h-7 flex-wrap items-center gap-x-3 gap-y-1 px-1 pt-1.5 text-micro text-muted-foreground" data-testid="orch-status-strip">
      <span className="inline-flex items-center gap-1">
        <span className={cn("size-1.5 rounded-full", v?.running ? "animate-pulse bg-emerald-500" : v?.attention.bucket === "needs_you" ? "bg-amber-500" : "bg-muted-foreground/50")} />
        {orchestrated && v?.phase ? phaseLabel(v.phase) : statusText(issue.status)}
        {!orchestrated && <span className="text-muted-foreground/60">· {t(($) => $.chat.manual)}</span>}
      </span>
      {v?.running && (
        <span>
          {v.running.agent} · {formatDuration(since(v.running.started_at ?? v.running.created_at))}
        </span>
      )}
      {v?.attempts && <span>{t(($) => $.chat.attempts, { impl: v.attempts.impl, review: v.attempts.review })}</span>}
      {timeline?.waiting && !v?.running && <span className="max-w-72 truncate">{t(($) => $.chat.waiting, { reason: timeline.waiting })}</span>}
      {v?.risk && (
        <span className={cn("inline-flex items-center gap-0.5", v.risk === "high" && "text-red-600")}>
          <ShieldAlert className="size-3" />
          {t(($) => $.chat.risk_label, { risk: v.risk })}
        </span>
      )}
      <span className="ml-auto inline-flex items-center gap-2">
        {wt && (wt.branch || wt.path) && (
          <button
            type="button"
            onClick={() => copy(wt.path || wt.branch || "")}
            title={wt.path || wt.branch}
            aria-label={t(($) => $.chat.copy_path)}
            className="inline-flex max-w-56 items-center gap-1 rounded-sm px-1 font-mono hover:bg-muted hover:text-foreground"
            data-testid="orch-worktree"
          >
            <FolderGit2 className="size-3 shrink-0" />
            <span className="truncate">{wt.branch || wt.path}</span>
            <Copy className="size-3 shrink-0 opacity-60" />
          </button>
        )}
        <DropdownMenu>
          <DropdownMenuTrigger
            className="inline-flex items-center gap-1 rounded-sm px-1 hover:bg-muted hover:text-foreground"
            aria-label={t(($) => $.actions.priority)}
          >
            <PriorityIcon priority={issue.priority} className="size-3" />
            {ti(($) => $.priority[issue.priority as IssuePriority] ?? $.priority.none)}
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" side="top">
            <DropdownMenuGroup>
              <DropdownMenuLabel>{t(($) => $.actions.priority)}</DropdownMenuLabel>
              <DropdownMenuRadioGroup value={issue.priority} onValueChange={(p) => updateIssue.mutate({ id: issue.id, priority: p as IssuePriority })}>
                {PRIORITIES.map((p) => (
                  <DropdownMenuRadioItem key={p} value={p}>
                    <PriorityIcon priority={p} className="size-3.5" />
                    {ti(($) => $.priority[p])}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
        <DropdownMenu>
          <DropdownMenuTrigger className="rounded-sm p-0.5 hover:bg-muted hover:text-foreground" aria-label={t(($) => $.actions.slash_hint)} data-testid="orch-actions-menu">
            <MoreHorizontal className="size-3.5" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" side="top" className="min-w-48">
            <DropdownMenuGroup>
              <DropdownMenuLabel>{t(($) => $.actions.risk)}</DropdownMenuLabel>
              <DropdownMenuRadioGroup value={v?.risk ?? ""} onValueChange={(r) => setRisk.mutate(r ? (r as (typeof RISKS)[number]) : null)}>
                <DropdownMenuRadioItem value="">{t(($) => $.actions.risk_none)}</DropdownMenuRadioItem>
                {RISKS.map((r) => (
                  <DropdownMenuRadioItem key={r} value={r}>
                    {r === "low" ? t(($) => $.actions.risk_low) : r === "medium" ? t(($) => $.actions.risk_medium) : t(($) => $.actions.risk_high)}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuGroup>
            {orchestrated && !settled && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                  {v?.paused ? (
                    <DropdownMenuItem disabled={pending} onClick={() => void run("resume")}>
                      <Play className="size-3.5" />
                      {t(($) => $.actions.resume)}
                    </DropdownMenuItem>
                  ) : (
                    <DropdownMenuItem disabled={pending} onClick={() => void run("pause")}>
                      <Pause className="size-3.5" />
                      {t(($) => $.actions.pause)}
                    </DropdownMenuItem>
                  )}
                  <DropdownMenuItem
                    variant="destructive"
                    disabled={pending}
                    onClick={() => {
                      if (window.confirm(t(($) => $.actions.cancel_confirm, { key }))) void run("cancel");
                    }}
                  >
                    <Ban className="size-3.5" />
                    {t(($) => $.actions.cancel)}
                  </DropdownMenuItem>
                </DropdownMenuGroup>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </span>
    </div>
  );
}
