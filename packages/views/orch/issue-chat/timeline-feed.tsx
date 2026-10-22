"use client";

import { useState } from "react";
import { AlertCircle, ChevronRight } from "lucide-react";
import type { TimelineEntry } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { cn } from "@multica/ui/lib/utils";
import { useLocale, useT, useTimeAgo } from "../../i18n";
import { formatActivity } from "../../issues/components/issue-detail";
import type { CommentRun } from "../../issues/components/comment-runs";
import { RetryRunButton } from "../../issues/components/retry-run-button";
import { Collapse } from "../parts/collapse";
import { dedupeEvents } from "./build-items";
import { HandoffCard, useStatusText, type OrchHandoff } from "./timeline-parts";

// Row geometry shared by the feed and the failure card, matching the commit
// graph: a fixed-height row, a 14px rail with a dot, caption text.
const RAIL = "relative w-3.5 shrink-0 self-stretch before:absolute before:inset-y-0 before:left-1/2 before:w-px before:-translate-x-1/2 before:bg-border";
const DOT = "absolute top-1/2 left-1/2 size-1.5 -translate-x-1/2 -translate-y-1/2 rounded-full ring-2 ring-background";

const TONE: Record<string, string> = {
  ok: "bg-emerald-500",
  warn: "bg-amber-500",
  bad: "bg-red-500",
  info: "bg-sky-500",
  muted: "bg-muted-foreground/50",
};

// Engine transition ids → tone. The label itself comes from orch.flow_tid.
function tidTone(tid: string): keyof typeof TONE {
  if (/^T4_|^T_epic_done|^T11a/.test(tid)) return "ok";
  if (/^T6_|^T8|^T11b|cancel/.test(tid)) return "bad";
  if (/^T10_|^T_epic_acceptance|^T5q_|^T3_|^T11_/.test(tid)) return "warn";
  if (/^T1_|^T2_|^T0_|^T3b|^T7/.test(tid)) return "info";
  return "muted";
}

export function useTidLabel() {
  const { t } = useT("orch");
  return (tid: string) => {
    const key = tid as keyof typeof FLOW_TID_KEYS;
    if (key in FLOW_TID_KEYS) return t(($) => $.flow_tid[key]);
    return tid.replace(/^T\d+[a-z]?_/, "").replace(/_/g, " ");
  };
}

// The transition ids the UI names; anything else falls back to the id with
// its T-number stripped.
const FLOW_TID_KEYS = {
  T0_dispatch: 1, T1_start: 1, T2_review: 1, T3_rework: 1, T3b_rereview: 1, T3c_carry_verdict: 1, T4_done: 1,
  T5_rerun: 1, T5_rerun_nobranch: 1, T5_rerun_norun: 1, T5b_cancel_stuck: 1, T5q_provider_limit: 1, T6_block: 1,
  T7_unblock: 1, T7b_unblock_escalate: 1, T8_cancel: 1, T8a_cancel_proposed: 1, T8b_member_cancel: 1, T9_reconcile: 1,
  T10_await_acceptance: 1, T11_resolve_conflict: 1, T11a_conflict_resolved: 1, T11b_conflict_handback: 1, T_epic_progress: 1, T_epic_acceptance: 1, T_epic_done: 1, T_expert_consulted: 1,
} as const;

function Reason({ text }: { text: string }) {
  const [open, setOpen] = useState(false);
  const long = text.length > 90;
  return (
    <button
      type="button"
      onClick={() => long && setOpen((v) => !v)}
      className={cn("min-w-0 text-left text-faint-foreground", long && "cursor-pointer hover:text-muted-foreground", open ? "whitespace-pre-wrap break-words" : "truncate")}
      title={long && !open ? text : undefined}
      aria-expanded={long ? open : undefined}
    >
      {text}
    </button>
  );
}

function EventRow({ entry }: { entry: TimelineEntry }) {
  const { t } = useT("orch");
  const { t: ti } = useT("issues");
  const locale = useLocale();
  const timeAgo = useTimeAgo();
  const statusText = useStatusText();
  const tidLabel = useTidLabel();
  const { getActorName } = useActorName();
  const d = (entry.details ?? {}) as Record<string, string | undefined>;
  const isTransition = entry.action === "orchestrator_transition";
  const handoff = isTransition ? (entry.details as { handoff?: OrchHandoff } | undefined)?.handoff : undefined;
  let tone: keyof typeof TONE = "muted";
  let head: string;
  const meta: string[] = [];
  if (isTransition) {
    tone = tidTone(d.tid ?? "");
    head = tidLabel(d.tid ?? "");
    if (d.status) meta.push(t(($) => $.chat.moved_to, { status: statusText(d.status) }));
    if (d.assignee) meta.push(t(($) => $.chat.assigned_to, { who: d.assignee }));
  } else {
    head = `${entry.actor_name || getActorName(entry.actor_type, entry.actor_id)} ${formatActivity(entry, ti, locale, getActorName, statusText)}`;
  }
  return (
    <li className="flex flex-col" data-testid="orch-event" data-tid={d.tid}>
      <div className="flex min-h-6 items-stretch gap-2">
        <span className={RAIL} aria-hidden>
          <span className={cn(DOT, TONE[tone])} />
        </span>
        <div className="flex min-w-0 flex-1 items-center gap-1.5 py-0.5 text-caption">
          <span className={cn("shrink-0", isTransition ? "font-medium text-foreground" : "text-muted-foreground")}>{head}</span>
          {meta.length > 0 && <span className="shrink-0 text-muted-foreground">{meta.join(" · ")}</span>}
          {isTransition && d.why && <Reason text={d.why} />}
          <span className="ml-auto shrink-0 pl-2 text-micro text-faint-foreground" title={entry.created_at}>{timeAgo(entry.created_at)}</span>
        </div>
      </div>
      {handoff && typeof handoff === "object" && handoff.kind && (
        <div className="flex gap-2">
          <span className={RAIL} aria-hidden />
          <div className="min-w-0 flex-1 [&>div]:mx-0 [&>div]:max-w-none">
            <HandoffCard handoff={handoff} />
          </div>
        </div>
      )}
    </li>
  );
}

const VISIBLE = 3;

// EventsFeed: a burst of engine transitions and activities as compact rows on
// a rail. Raw transition ids become words, long reasons truncate and expand
// in place, and a long burst shows its last rows with the rest folded.
export function EventsFeed({ entries }: { entries: TimelineEntry[] }) {
  const { t } = useT("orch");
  const rows = dedupeEvents(entries);
  const [open, setOpen] = useState(false);
  if (rows.length === 0) return null;
  const hidden = rows.length > VISIBLE + 1 && !open ? rows.length - VISIBLE : 0;
  return (
    <ol className="my-1 flex flex-col" data-testid="orch-events">
      {hidden > 0 && (
        <li className="flex min-h-6 items-stretch gap-2">
          <span className={RAIL} aria-hidden />
          <button
            type="button"
            onClick={() => setOpen(true)}
            className="flex items-center gap-1 text-caption text-muted-foreground transition-colors hover:text-foreground"
          >
            <ChevronRight className="size-3" />
            {t(($) => $.feed.earlier, { count: hidden })}
          </button>
        </li>
      )}
      {rows.slice(hidden).map((e) => (
        <EventRow key={e.id} entry={e} />
      ))}
    </ol>
  );
}

// FailureGroupCard: N failed runs that ended with the same error, as one
// card — count, agents, time span, the error once, one retry (of the latest
// run), and the individual runs plus the churn between them on demand.
export function FailureGroupCard({ runs, between, issueId }: { runs: CommentRun[]; between: TimelineEntry[]; issueId: string }) {
  const { t } = useT("orch");
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const [open, setOpen] = useState(false);
  const first = runs[0]!.task;
  const last = runs[runs.length - 1]!.task;
  const agents = [...new Set(runs.map((r) => getActorName("agent", r.task.agent_id)))];
  const endAt = (task: CommentRun["task"]) => task.completed_at ?? task.created_at;
  const err = (first.error ?? "").trim();
  return (
    <div className="my-1.5 overflow-hidden rounded-lg border border-red-500/25 bg-red-500/[0.03]" data-testid="orch-failure-group">
      <div className="flex items-start gap-2 px-3 py-2">
        <AlertCircle className="mt-0.5 size-4 shrink-0 text-red-600 dark:text-red-400" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-1.5 text-body">
            <span className="font-medium text-foreground">{t(($) => $.failures.title, { count: runs.length })}</span>
            {first.failure_reason && <span className="rounded-sm bg-muted px-1 font-mono text-micro text-muted-foreground">{first.failure_reason}</span>}
          </div>
          <div className="mt-0.5 truncate text-caption text-muted-foreground">
            {agents.join(", ")} · {timeAgo(first.created_at)} – {timeAgo(endAt(last))}
          </div>
          {err && <pre className="mt-1.5 max-h-24 overflow-auto rounded-md bg-muted/60 px-2 py-1.5 font-mono text-micro whitespace-pre-wrap break-words text-foreground/80">{err}</pre>}
        </div>
        <div className="flex max-w-[40%] min-w-0 shrink-0 justify-end">
          <RetryRunButton task={last} issueId={issueId} />
        </div>
      </div>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex w-full items-center gap-1 border-t border-red-500/15 px-3 py-1 text-caption text-muted-foreground transition-colors hover:bg-red-500/5 hover:text-foreground"
      >
        <ChevronRight className={cn("size-3 transition-transform", open && "rotate-90")} />
        {t(($) => $.failures.show_runs, { count: runs.length })}
      </button>
      <Collapse open={open}>
        <ol className="flex flex-col px-3 pb-2">
          {[
            ...runs.map((r) => ({ at: r.task.created_at, run: r, entry: undefined as TimelineEntry | undefined })),
            ...dedupeEvents(between).map((e) => ({ at: e.created_at, run: undefined as CommentRun | undefined, entry: e })),
          ]
            .sort((a, b) => a.at.localeCompare(b.at))
            .map((x) =>
              x.run ? (
                <li key={x.run.task.id} className="flex min-h-6 items-stretch gap-2 text-caption">
                  <span className={RAIL} aria-hidden>
                    <span className={cn(DOT, TONE.bad)} />
                  </span>
                  <span className="flex min-w-0 flex-1 items-center gap-1.5">
                    <span className="shrink-0 text-foreground">{getActorName("agent", x.run.task.agent_id)}</span>
                    <span className="text-red-600/80 dark:text-red-400/80">{t(($) => $.failures.failed)}</span>
                    <span className="font-mono text-micro text-faint-foreground">{x.run.task.id.slice(0, 8)}</span>
                    <span className="ml-auto shrink-0 text-micro text-faint-foreground">{timeAgo(endAt(x.run.task))}</span>
                  </span>
                </li>
              ) : (
                <EventRow key={x.entry!.id} entry={x.entry!} />
              ),
            )}
        </ol>
      </Collapse>
    </div>
  );
}
