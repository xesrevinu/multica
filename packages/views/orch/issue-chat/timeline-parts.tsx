"use client";

import { useState } from "react";
import { ChevronRight, ExternalLink, GitCommitHorizontal, Loader2 } from "lucide-react";
import { isBuiltInIssueStatus } from "@multica/core/issue-statuses";
import type { TimelineEntry } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { cn } from "@multica/ui/lib/utils";
import { useLocale, useT, useTimeAgo } from "../../i18n";
import { formatActivity } from "../../issues/components/issue-detail";
import { AgentRunComment } from "../../issues/components/comment-card";
import type { CommentRun } from "../../issues/components/comment-runs";
import { formatDuration } from "../format";
import { ReadonlyContent } from "../../editor";
import { Collapse } from "../parts/collapse";
import { PersonAvatar } from "../parts/person-avatar";
import { runDurationMs, type GitHubReviewItem } from "./build-items";

export function useStatusText() {
  const { t } = useT("issues");
  return (status: string | undefined) => (status && isBuiltInIssueStatus(status) ? t(($) => $.status[status]) : (status ?? ""));
}

// TransitionDivider: an orchestrator transition as a centered system label
// between two hairlines (t3code TimelineSystemDivider).
// Handoff mirrors orchestrator.Handoff: the structured send-back context the
// next run received (review findings, failing checks, failure class, attempts).
export type OrchHandoff = {
  kind: string;
  summary?: string;
  failure_reason?: string;
  to?: string;
  role?: string;
  attempt?: number;
  prev_head?: string;
  pr?: string;
  diff_summary?: string;
  prev_run?: { id: string; status: string; error?: string };
  review_comments?: { source: string; author?: string; path?: string; line?: number; body: string }[];
  checks?: { name: string; conclusion: string; url?: string; annotations?: string[]; log_excerpt?: string }[];
};

export function HandoffCard({ handoff: h }: { handoff: OrchHandoff }) {
  const { t } = useT("orch");
  return (
    <div className="mx-auto my-1 w-full max-w-[92%] rounded-lg border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-caption" data-testid="orch-handoff">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
        <span className="font-medium text-foreground">{t(($) => $.chat.handoff_title, { kind: h.kind.replace(/_/g, " ") })}</span>
        {h.to && <span className="text-muted-foreground">→ {h.to}</span>}
        {typeof h.attempt === "number" && h.attempt > 0 && <span className="text-muted-foreground">· {t(($) => $.chat.handoff_attempt, { n: h.attempt })}</span>}
        {h.prev_head && <span className="font-mono text-faint-foreground">· {h.prev_head.slice(0, 8)}</span>}
        {h.failure_reason && <span className="font-mono text-faint-foreground">· {h.failure_reason}</span>}
      </div>
      {h.summary && <p className="mt-1 break-words text-muted-foreground">{h.summary}</p>}
      {h.diff_summary && <p className="mt-1 break-words text-faint-foreground">{t(($) => $.chat.handoff_diff)}: {h.diff_summary}</p>}
      {!!h.review_comments?.length && (
        <div className="mt-1.5">
          <div className="font-medium text-foreground">{t(($) => $.chat.handoff_review)}</div>
          <ul className="mt-0.5 space-y-0.5">
            {h.review_comments.map((c, i) => (
              <li key={i} className="break-words text-muted-foreground">
                {c.path && <span className="font-mono text-faint-foreground">{c.path}{c.line ? `:${c.line}` : ""} </span>}
                <span className="line-clamp-3">{c.body}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {!!h.checks?.length && (
        <div className="mt-1.5">
          <div className="font-medium text-foreground">{t(($) => $.chat.handoff_checks)}</div>
          <ul className="mt-0.5 space-y-0.5">
            {h.checks.map((c) => (
              <li key={c.name} className="break-words text-muted-foreground">
                {c.url ? (
                  <a href={c.url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 hover:text-foreground">
                    {c.name} <ExternalLink className="size-3" />
                  </a>
                ) : (
                  c.name
                )}{" "}
                <span className="text-faint-foreground">{c.conclusion}</span>
                {c.annotations?.slice(0, 3).map((a, i) => (
                  <div key={i} className="truncate font-mono text-micro text-faint-foreground">{a}</div>
                ))}
              </li>
            ))}
          </ul>
        </div>
      )}
      {h.prev_run?.error && (
        <details className="mt-1.5">
          <summary className="cursor-pointer text-muted-foreground">{t(($) => $.chat.handoff_prev_run)} ({h.prev_run.status})</summary>
          <pre className="mt-1 max-h-40 overflow-auto whitespace-pre-wrap font-mono text-micro text-faint-foreground">{h.prev_run.error}</pre>
        </details>
      )}
    </div>
  );
}

export function TransitionDivider({ entry }: { entry: TimelineEntry }) {
  const handoff = (entry.details as { handoff?: OrchHandoff } | undefined)?.handoff;
  return (
    <>
      <TransitionLine entry={entry} />
      {handoff && typeof handoff === "object" && handoff.kind && <HandoffCard handoff={handoff} />}
    </>
  );
}

function TransitionLine({ entry }: { entry: TimelineEntry }) {
  const { t } = useT("orch");
  const statusText = useStatusText();
  const timeAgo = useTimeAgo();
  const d = (entry.details ?? {}) as Record<string, string | undefined>;
  const parts: string[] = [];
  if (d.status) parts.push(t(($) => $.chat.moved_to, { status: statusText(d.status) }));
  if (d.assignee) parts.push(t(($) => $.chat.assigned_to, { who: d.assignee }));
  return (
    <div className="flex items-center gap-3 py-3" data-testid="orch-transition">
      <span className="h-px flex-1 bg-border/70" />
      <span className="inline-flex max-w-[80%] items-center gap-1.5 text-micro text-muted-foreground" title={d.why || undefined}>
        <GitCommitHorizontal className="size-3 shrink-0 text-faint-foreground" />
        {d.tid && <span className="font-mono">{d.tid.replace(/^T\d+_/, "")}</span>}
        {parts.length > 0 && <span className="truncate">{parts.join(" · ")}</span>}
        <span className="shrink-0 text-faint-foreground">· {timeAgo(entry.created_at)}</span>
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
      <Collapse open={open}>
        <ul className="mt-1 ml-1.5 flex flex-col gap-0.5 border-l border-border/60 pl-3">
          {entries.map((e) => (
            <li key={e.id} className="flex items-baseline gap-1.5 text-caption text-muted-foreground">
              <span className="font-medium text-foreground">{e.actor_name || getActorName(e.actor_type, e.actor_id)}</span>
              <span className="min-w-0 truncate">{formatActivity(e, t, locale, getActorName, statusText)}</span>
              <span className="ml-auto shrink-0 text-micro text-faint-foreground">{timeAgo(e.created_at)}</span>
            </li>
          ))}
        </ul>
      </Collapse>
    </div>
  );
}

// WorkedForFold: a run without its own reply comment, folded to t3code's
// "Worked for 1m 17s ›" row (hairline under it, time on the right),
// expanding to the full run transcript.
export function WorkedForFold({ run, entering, agentName }: { run: CommentRun; entering: boolean; agentName: string }) {
  const { t } = useT("orch");
  const timeAgo = useTimeAgo();
  const status = run.task.status;
  const active = status === "running" || status === "queued" || status === "dispatched" || status === "waiting_local_directory" || status === "deferred";
  const queued = status === "queued" || status === "dispatched" || status === "deferred";
  const [open, setOpen] = useState(active && !queued);
  const duration = formatDuration(runDurationMs(run));
  const label = queued
    ? t(($) => $.chat.queued_short)
    : active
      ? t(($) => $.chat.working_short, { duration })
      : status === "failed"
        ? t(($) => $.chat.failed_short, { duration })
        : status === "cancelled"
          ? t(($) => $.chat.cancelled_short, { duration })
          : t(($) => $.chat.worked_for_short, { duration });
  const at = (run.task as { completed_at?: string | null; created_at?: string }).completed_at ?? run.task.created_at;
  return (
    <div className="py-0.5" data-testid="orch-run-fold">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className={cn(
          "group/fold flex w-full items-center gap-1.5 border-b border-border pb-1.5 text-left text-body text-muted-foreground transition-colors hover:text-foreground",
          status === "failed" && "text-red-600 dark:text-red-400",
        )}
      >
        {active && !queued && <Loader2 className="size-3.5 shrink-0 animate-spin text-sky-600 dark:text-sky-400" />}
        <span className="shrink-0 text-foreground">{agentName}</span>
        <span className="truncate">{label}</span>
        <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", open && "rotate-90")} />
        {at && <span className="ml-auto shrink-0 text-caption text-faint-foreground">{timeAgo(at)}</span>}
      </button>
      <Collapse open={open}>
        <div className="mt-2 max-h-[min(24rem,50vh)] overflow-y-auto">
          <AgentRunComment run={run} entering={entering} standalone />
        </div>
      </Collapse>
    </div>
  );
}

const GITHUB = "GitHub";

// GitHubReviewMessage: a PR review or inline review comment from GitHub,
// rendered like an agent message with a file:line chip.
export function GitHubReviewMessage({ review }: { review: GitHubReviewItem }) {
  const { t } = useT("orch");
  const timeAgo = useTimeAgo();
  const stateKey = (review.state ?? "") as "APPROVED" | "CHANGES_REQUESTED" | "COMMENTED" | "DISMISSED";
  const verb = review.type === "review" && review.state ? t(($) => $.pr.review_state[stateKey] ?? $.pr.review_state.COMMENTED) : undefined;
  return (
    <div className="px-1 py-1" data-testid="orch-gh-review">
      <div className="flex items-center gap-1.5 text-caption">
        <PersonAvatar name={review.user} url={review.avatar} />
        <span className="text-body font-medium text-foreground">{review.user}</span>
        {verb && (
          <span className={cn("text-muted-foreground", stateKey === "APPROVED" && "text-emerald-700 dark:text-emerald-300", stateKey === "CHANGES_REQUESTED" && "text-red-700 dark:text-red-300")}>
            {verb}
          </span>
        )}
        {review.path && (
          <span className="inline-flex max-w-64 items-center truncate rounded-md border border-border bg-muted px-1.5 font-mono text-micro text-foreground" title={review.path}>
            {review.path}
            {review.line ? `:${review.line}` : ""}
          </span>
        )}
        <span className="text-faint-foreground">{`· ${GITHUB} · ${timeAgo(review.at)}`}</span>
        {review.url && (
          <a href={review.url} target="_blank" rel="noreferrer" className="ml-auto text-muted-foreground hover:text-foreground" aria-label={t(($) => $.pr.open_on_github)}>
            <ExternalLink className="size-3.5" />
          </a>
        )}
      </div>
      <ReadonlyContent content={review.body} className="mt-1 pl-[26px] text-body" />
      {(review.replies ?? []).length > 0 && (
        <ul className="mt-2 ml-[26px] flex flex-col gap-2 border-l border-border pl-3">
          {review.replies!.map((r, i) => (
            <li key={`${r.at}-${i}`}>
              <p className="text-caption">
                <span className="font-medium text-foreground">{r.user}</span> <span className="text-faint-foreground">· {timeAgo(r.at)}</span>
              </p>
              <ReadonlyContent content={r.body} className="text-body" />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
