"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Ban, Bot, ChevronDown, Copy, FolderGit2, GitBranch, Laptop, MoreHorizontal, Pause, Play, ShieldAlert, ShieldCheck } from "lucide-react";
import type { Issue, IssuePriority } from "@multica/core/types";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { diffOf, useOrchSetRisk, type OrchTimeline } from "@multica/core/orchestrator";
import { agentListOptions } from "@multica/core/workspace/queries";
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
import { DiffStat } from "../parts/chips";
import { useFlowAction } from "./flow-cards";
import { useStatusText } from "./timeline-parts";

const PRIORITIES: IssuePriority[] = ["urgent", "high", "medium", "low", "none"];
const RISKS = ["low", "medium", "high"] as const;

const CHIP =
  "inline-flex h-7 max-w-44 shrink-0 items-center gap-1 rounded-full px-2 text-caption text-muted-foreground transition-colors hover:bg-muted hover:text-foreground aria-expanded:bg-muted aria-expanded:text-foreground";

// ComposerChips: the t3code "model · effort · access" chips, mapped onto the
// orchestrator — the agent on the issue (with its model), the priority and
// the risk tier (which decides how much the flow may do unattended).
export function ComposerChips({ wsId, issue, timeline }: { wsId: string; issue: Issue; timeline: OrchTimeline | null | undefined }) {
  const { t } = useT("orch");
  const { t: ti } = useT("issues");
  const v = timeline?.issue;
  const updateIssue = useUpdateIssue();
  const setRisk = useOrchSetRisk(wsId, issue.id);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const agentId = v?.assignee?.type === "agent" ? v.assignee.id : issue.assignee_type === "agent" ? issue.assignee_id : null;
  const agent = agentId ? agents.find((a) => a.id === agentId) : undefined;
  const agentName = v?.running?.agent || agent?.name || (v?.assignee?.type === "agent" ? v.assignee.name : "");
  const model = (agent as { model?: string } | undefined)?.model;
  const risk = v?.risk;
  return (
    <span className="flex min-w-0 items-center gap-0.5" data-testid="orch-composer-chips">
      <span className={cn(CHIP, "cursor-default hover:bg-transparent")} title={t(($) => $.chat.agent_title)}>
        <Bot className="size-3.5 shrink-0" />
        <span className="truncate text-foreground">{agentName || t(($) => $.chat.no_agent)}</span>
        {model && <span className="hidden truncate sm:inline">· {model}</span>}
      </span>
      <DropdownMenu>
        <DropdownMenuTrigger className={CHIP} aria-label={t(($) => $.actions.priority)} data-testid="orch-chip-priority">
          <PriorityIcon priority={issue.priority} className="size-3" />
          <span className="truncate">{ti(($) => $.priority[issue.priority as IssuePriority] ?? $.priority.none)}</span>
          <ChevronDown className="size-3 shrink-0" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" side="top">
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
        <DropdownMenuTrigger
          className={cn(CHIP, risk === "high" && "text-red-600 dark:text-red-400")}
          aria-label={t(($) => $.actions.risk)}
          data-testid="orch-chip-risk"
        >
          {risk === "high" ? <ShieldAlert className="size-3.5 shrink-0" /> : <ShieldCheck className="size-3.5 shrink-0" />}
          <span className="hidden truncate sm:inline">{risk ? t(($) => $.chat.risk_label, { risk }) : t(($) => $.actions.risk_none)}</span>
          <ChevronDown className="size-3 shrink-0" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" side="top">
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t(($) => $.actions.risk)}</DropdownMenuLabel>
            <DropdownMenuRadioGroup value={risk ?? ""} onValueChange={(r) => setRisk.mutate(r ? (r as (typeof RISKS)[number]) : null)}>
              <DropdownMenuRadioItem value="">{t(($) => $.actions.risk_none)}</DropdownMenuRadioItem>
              {RISKS.map((r) => (
                <DropdownMenuRadioItem key={r} value={r}>
                  {r === "low" ? t(($) => $.actions.risk_low) : r === "medium" ? t(($) => $.actions.risk_medium) : t(($) => $.actions.risk_high)}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </span>
  );
}

// StatusStrip: the context bar tucked under the composer (t3code's
// "Local checkout · +9 · branch" strip) — checkout mode, diff, branch on the
// left; flow phase, who is working and the member actions on the right.
export function StatusStrip({ wsId, issue, timeline }: { wsId: string; issue: Issue; timeline: OrchTimeline | null | undefined }) {
  const { t } = useT("orch");
  const phaseLabel = usePhaseLabel();
  const statusText = useStatusText();
  const v = timeline?.issue;
  const key = issue.identifier;
  const { run, pending } = useFlowAction(wsId, key);
  const settled = issue.status === "done" || issue.status === "cancelled";
  const orchestrated = !!v?.orchestrated;
  const wt = v?.worktree;
  const branch = wt?.branch || v?.pr?.branch;
  const diff = v ? diffOf(v) : { additions: 0, deletions: 0 };
  const local = v?.mode === "local";
  const copy = (text: string) => {
    void navigator.clipboard?.writeText(text).then(() => toast.success(t(($) => $.chat.copied)));
  };
  const ModeIcon = local ? Laptop : FolderGit2;
  return (
    <div
      className="relative z-0 mx-[1.375rem] -mt-4 flex min-h-8 flex-wrap items-center gap-x-2.5 gap-y-1 rounded-b-2xl border border-t-0 border-border bg-muted/40 px-3 pt-5 pb-1.5 text-caption text-muted-foreground max-md:mx-3"
      data-testid="orch-status-strip"
    >
      <button
        type="button"
        onClick={() => wt?.path && copy(wt.path)}
        disabled={!wt?.path}
        title={wt?.path || undefined}
        aria-label={t(($) => $.chat.copy_path)}
        className="inline-flex items-center gap-1 rounded-sm hover:text-foreground disabled:hover:text-muted-foreground"
        data-testid="orch-worktree"
      >
        <ModeIcon className="size-3.5 shrink-0" />
        {orchestrated ? (local ? t(($) => $.chat.mode_local) : t(($) => $.chat.mode_worktree)) : t(($) => $.chat.mode_manual)}
      </button>
      <DiffStat additions={diff.additions} deletions={diff.deletions} className="text-caption" />
      <span className="inline-flex min-w-0 items-center gap-1">
        <GitBranch className="size-3.5 shrink-0" />
        {branch ? (
          <button
            type="button"
            onClick={() => copy(branch)}
            className="max-w-56 truncate font-mono text-micro hover:text-foreground"
            title={branch}
          >
            {branch}
          </button>
        ) : (
          <span className="text-faint-foreground">{t(($) => $.chat.no_branch)}</span>
        )}
        {branch && <Copy className="size-3 shrink-0 text-faint-foreground" aria-hidden />}
      </span>
      <span className="ml-auto inline-flex min-w-0 items-center gap-2">
        {timeline?.waiting && !v?.running && v?.attention.bucket !== "needs_you" && <span className="hidden max-w-60 truncate md:inline">{t(($) => $.chat.waiting, { reason: timeline.waiting })}</span>}
        {v?.attempts && (v.attempts.impl > 1 || v.attempts.review > 1) && (
          <span className="hidden sm:inline">{t(($) => $.chat.attempts, { impl: v.attempts.impl, review: v.attempts.review })}</span>
        )}
        <span className="inline-flex items-center gap-1">
          <span
            className={cn(
              "size-1.5 rounded-full",
              v?.running ? "animate-pulse bg-sky-500" : v?.attention.bucket === "needs_you" ? "bg-amber-500" : settled ? "bg-emerald-500" : "bg-border",
            )}
          />
          {orchestrated && v?.phase ? phaseLabel(v.phase) : statusText(issue.status)}
          {v?.running && <span className="text-faint-foreground">· {formatDuration(since(v.running.started_at ?? v.running.created_at))}</span>}
        </span>
        <DropdownMenu>
          <DropdownMenuTrigger className="rounded-sm p-0.5 hover:bg-muted hover:text-foreground" aria-label={t(($) => $.actions.slash_hint)} data-testid="orch-actions-menu">
            <MoreHorizontal className="size-3.5" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" side="top" className="min-w-48">
            {wt?.path && (
              <DropdownMenuItem onClick={() => copy(wt.path ?? "")}>
                <Copy className="size-3.5" />
                {t(($) => $.chat.copy_path)}
              </DropdownMenuItem>
            )}
            {orchestrated && !settled && (
              <>
                {wt?.path && <DropdownMenuSeparator />}
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
            {!(orchestrated && !settled) && !wt?.path && (
              <DropdownMenuItem disabled>{t(($) => $.chat.manual)}</DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </span>
    </div>
  );
}
