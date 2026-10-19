"use client";

import { useState } from "react";
import { ChevronRight, GitCommitHorizontal } from "lucide-react";
import { isBuiltInIssueStatus } from "@multica/core/issue-statuses";
import type { TimelineEntry } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { cn } from "@multica/ui/lib/utils";
import { useLocale, useT, useTimeAgo } from "../../i18n";
import { formatActivity } from "../../issues/components/issue-detail";
import { AgentRunComment } from "../../issues/components/comment-card";
import type { CommentRun } from "../../issues/components/comment-runs";
import { formatDuration } from "../format";
import { runDurationMs } from "./build-items";

export function useStatusText() {
  const { t } = useT("issues");
  return (status: string | undefined) => (status && isBuiltInIssueStatus(status) ? t(($) => $.status[status]) : (status ?? ""));
}

// TransitionDivider: an orchestrator transition as a centered system pill
// between two hairlines (t3code TimelineSystemDivider).
export function TransitionDivider({ entry }: { entry: TimelineEntry }) {
  const { t } = useT("orch");
  const statusText = useStatusText();
  const timeAgo = useTimeAgo();
  const d = (entry.details ?? {}) as Record<string, string | undefined>;
  const parts: string[] = [];
  if (d.status) parts.push(t(($) => $.chat.moved_to, { status: statusText(d.status) }));
  if (d.assignee) parts.push(t(($) => $.chat.assigned_to, { who: d.assignee }));
  return (
    <div className="flex items-center gap-3 py-2" data-testid="orch-transition">
      <span className="h-px flex-1 bg-border/70" />
      <span
        className="inline-flex max-w-[80%] items-center gap-1.5 rounded-full border border-border/70 bg-muted/30 px-2.5 py-0.5 text-micro text-muted-foreground"
        title={d.why || undefined}
      >
        <GitCommitHorizontal className="size-3 shrink-0" />
        {d.tid && <span className="font-mono text-muted-foreground">{d.tid}</span>}
        {parts.length > 0 && <span className="truncate">{parts.join(" · ")}</span>}
        {d.why && <span className="hidden truncate text-muted-foreground md:inline">— {d.why}</span>}
        <span className="shrink-0 text-faint-foreground">{timeAgo(entry.created_at)}</span>
      </span>
      <span className="h-px flex-1 bg-border/70" />
    </div>
  );
}

// ActivitiesFold: consecutive non-orchestrator activities as one muted line
// that expands in place.
export function ActivitiesFold({ entries, defaultOpen = false }: { entries: TimelineEntry[]; defaultOpen?: boolean }) {
  const { t } = useT("issues");
  const locale = useLocale();
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const statusText = useStatusText();
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="py-1">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex items-center gap-1.5 text-caption text-muted-foreground transition-colors hover:text-foreground"
      >
        <ChevronRight className={cn("size-3 shrink-0 transition-transform", open && "rotate-90")} />
        <span>{t(($) => $.activity.activity_count, { count: entries.length })}</span>
      </button>
      {open && (
        <ul className="mt-1 ml-1.5 flex flex-col gap-0.5 border-l border-border/60 pl-3">
          {entries.map((e) => (
            <li key={e.id} className="flex items-baseline gap-1.5 text-caption text-muted-foreground">
              <span className="font-medium text-foreground">{e.actor_name || getActorName(e.actor_type, e.actor_id)}</span>
              <span className="min-w-0 truncate">{formatActivity(e, t, locale, getActorName, statusText)}</span>
              <span className="ml-auto shrink-0 text-micro text-faint-foreground">{timeAgo(e.created_at)}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// WorkedForFold: a run without its own reply comment, folded to
// "agent worked for 3m" (t3code WorkLog), expanding to the full run card.
export function WorkedForFold({ run, entering, agentName }: { run: CommentRun; entering: boolean; agentName: string }) {
  const { t } = useT("orch");
  const status = run.task.status;
  const active = status === "running" || status === "queued" || status === "dispatched" || status === "waiting_local_directory" || status === "deferred";
  const [open, setOpen] = useState(active);
  const duration = formatDuration(runDurationMs(run));
  const label =
    status === "queued" || status === "dispatched" || status === "deferred"
      ? t(($) => $.chat.queued_run, { agent: agentName })
      : active
        ? t(($) => $.chat.running_for, { agent: agentName, duration })
        : status === "failed"
          ? t(($) => $.chat.run_failed, { agent: agentName, duration })
          : status === "cancelled"
            ? t(($) => $.chat.run_cancelled, { agent: agentName, duration })
            : t(($) => $.chat.worked_for, { agent: agentName, duration });
  return (
    <div className="py-1" data-testid="orch-run-fold">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className={cn(
          "flex w-full items-center gap-2 rounded-md border border-border/60 bg-muted/20 px-2.5 py-1.5 text-left text-caption text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground",
          status === "failed" && "border-red-500/30 text-red-700 dark:text-red-300",
        )}
      >
        <ChevronRight className={cn("size-3 shrink-0 transition-transform", open && "rotate-90")} />
        {active && <span className="size-1.5 shrink-0 animate-pulse rounded-full bg-emerald-500" />}
        <span className="truncate">{label}</span>
      </button>
      {open && (
        <div className="mt-2">
          <AgentRunComment run={run} entering={entering} standalone />
        </div>
      )}
    </div>
  );
}
