"use client";

import { useState } from "react";
import { AlertCircle, AlertTriangle, ArrowRight, CheckCircle2, ChevronRight, Dot, XCircle, type LucideIcon } from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@multica/ui/components/ui/card";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@multica/ui/components/ui/collapsible";
import { Item, ItemActions, ItemContent, ItemDescription, ItemFooter, ItemGroup, ItemMedia, ItemTitle } from "@multica/ui/components/ui/item";
import type { TimelineEntry } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { cn } from "@multica/ui/lib/utils";
import { useLocale, useT, useTimeAgo } from "../../i18n";
import { formatActivity } from "../../issues/components/issue-detail";
import type { CommentRun } from "../../issues/components/comment-runs";
import { RetryRunButton } from "../../issues/components/retry-run-button";
import { dedupeEvents } from "./build-items";
import { HandoffCard, useStatusText, type OrchHandoff } from "./timeline-parts";

// Engine transition ids → a semantic tone (design-system status tokens) and
// an icon. The label itself comes from orch.flow_tid.
type Tone = "success" | "warning" | "destructive" | "info" | "muted";
const TONE_TEXT: Record<Tone, string> = {
  success: "text-success",
  warning: "text-warning",
  destructive: "text-destructive",
  info: "text-info",
  muted: "text-muted-foreground",
};
const TONE_ICON: Record<Tone, LucideIcon> = {
  success: CheckCircle2,
  warning: AlertTriangle,
  destructive: XCircle,
  info: ArrowRight,
  muted: Dot,
};

function tidTone(tid: string): Tone {
  if (/^T4_|^T_epic_done|^T11a_/.test(tid)) return "success";
  if (/^T6_|^T8|cancel/.test(tid)) return "destructive";
  if (/^T10_|^T_epic_acceptance|^T5q_|^T3_|^T11b_/.test(tid)) return "warning";
  if (/^T1_|^T2_|^T0_|^T3b|^T7|^T11_/.test(tid)) return "info";
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
  if (!long) return <>{text}</>;
  return (
    <button
      type="button"
      onClick={() => setOpen((v) => !v)}
      className={cn("text-left hover:text-foreground", open ? "whitespace-pre-wrap break-words" : "line-clamp-1")}
      title={open ? undefined : text}
      aria-expanded={open}
    >
      {text}
    </button>
  );
}

// EventItem: one engine transition or activity as a design-system Item —
// tone icon, label, assignment/status meta, the reason, relative time.
function EventItem({ entry }: { entry: TimelineEntry }) {
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
  let tone: Tone = "muted";
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
  const Icon = TONE_ICON[tone];
  const description = [meta.join(" · "), isTransition ? d.why : ""].filter(Boolean);
  return (
    <Item size="xs" role="listitem" data-testid="orch-event" data-tid={d.tid}>
      <ItemMedia className={TONE_TEXT[tone]}>
        <Icon className="size-3.5" aria-hidden />
      </ItemMedia>
      <ItemContent className="min-w-0">
        <ItemTitle className={cn(!isTransition && "font-normal text-muted-foreground")}>{head}</ItemTitle>
        {description.length > 0 && (
          <ItemDescription>
            {meta.length > 0 && <span>{meta.join(" · ")}</span>}
            {meta.length > 0 && isTransition && d.why && <span> · </span>}
            {isTransition && d.why && <Reason text={d.why} />}
          </ItemDescription>
        )}
      </ItemContent>
      <ItemActions className="self-start text-caption text-muted-foreground" title={entry.created_at}>
        {timeAgo(entry.created_at)}
      </ItemActions>
      {handoff && typeof handoff === "object" && handoff.kind && (
        <ItemFooter className="[&>div]:mx-0 [&>div]:max-w-none">
          <HandoffCard handoff={handoff} />
        </ItemFooter>
      )}
    </Item>
  );
}

const VISIBLE = 3;

// EventsFeed: a burst of engine transitions and activities as one small Card
// of Items. Raw transition ids become words, long reasons expand in place,
// and a long burst shows its last rows with the rest behind a footer button.
export function EventsFeed({ entries }: { entries: TimelineEntry[] }) {
  const { t } = useT("orch");
  const rows = dedupeEvents(entries);
  const [open, setOpen] = useState(false);
  if (rows.length === 0) return null;
  const hidden = rows.length > VISIBLE + 1 && !open ? rows.length - VISIBLE : 0;
  return (
    <Card size="sm" className="my-1.5 gap-0 py-0.5 data-[size=sm]:py-0.5" data-testid="orch-events">
      {hidden > 0 && (
        <CardContent className="px-1">
          <Button variant="ghost" size="xs" onClick={() => setOpen(true)} className="text-muted-foreground">
            <ChevronRight data-icon="inline-start" />
            {t(($) => $.feed.earlier, { count: hidden })}
          </Button>
        </CardContent>
      )}
      <CardContent className="px-1">
        <ItemGroup role="list">
          {rows.slice(hidden).map((e) => (
            <EventItem key={e.id} entry={e} />
          ))}
        </ItemGroup>
      </CardContent>
    </Card>
  );
}

// FailureGroupCard: N failed runs that ended with the same error, as one
// Card — count, agents, time span, the error once, one retry (of the latest
// run), and the individual runs plus the churn between them in a Collapsible.
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
    <Collapsible open={open} onOpenChange={setOpen} render={<Card size="sm" className="my-1.5" data-testid="orch-failure-group" />}>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-2">
          <AlertCircle className="size-4 shrink-0 text-destructive" aria-hidden />
          {t(($) => $.failures.title, { count: runs.length })}
          {first.failure_reason && <Badge variant="destructive">{first.failure_reason}</Badge>}
        </CardTitle>
        <CardDescription className="text-caption">
          {agents.join(", ")} · {timeAgo(first.created_at)} – {timeAgo(endAt(last))}
        </CardDescription>
      </CardHeader>
      {err && (
        <CardContent>
          <pre className="max-h-24 overflow-auto rounded-md bg-muted px-2 py-1.5 font-mono text-caption whitespace-pre-wrap break-words text-muted-foreground">{err}</pre>
        </CardContent>
      )}
      <CollapsibleContent>
        <CardContent className="px-1">
          <ItemGroup role="list">
            {[
              ...runs.map((r) => ({ at: r.task.created_at, run: r, entry: undefined as TimelineEntry | undefined })),
              ...dedupeEvents(between).map((e) => ({ at: e.created_at, run: undefined as CommentRun | undefined, entry: e })),
            ]
              .sort((a, b) => a.at.localeCompare(b.at))
              .map((x) =>
                x.run ? (
                  <Item key={x.run.task.id} size="xs" role="listitem">
                    <ItemMedia className="text-destructive">
                      <XCircle className="size-3.5" aria-hidden />
                    </ItemMedia>
                    <ItemContent className="min-w-0">
                      <ItemTitle>
                        {getActorName("agent", x.run.task.agent_id)}
                        <span className="font-normal text-destructive">{t(($) => $.failures.failed)}</span>
                        <span className="font-mono font-normal text-caption text-muted-foreground">{x.run.task.id.slice(0, 8)}</span>
                      </ItemTitle>
                    </ItemContent>
                    <ItemActions className="text-caption text-muted-foreground">{timeAgo(endAt(x.run.task))}</ItemActions>
                  </Item>
                ) : (
                  <EventItem key={x.entry!.id} entry={x.entry!} />
                ),
              )}
          </ItemGroup>
        </CardContent>
      </CollapsibleContent>
      <CardFooter className="justify-between gap-3 py-1.5 group-data-[size=sm]/card:py-1.5">
        <CollapsibleTrigger render={<Button variant="ghost" size="xs" className="text-muted-foreground" />}>
          <ChevronRight data-icon="inline-start" className={cn("transition-transform", open && "rotate-90")} />
          {t(($) => $.failures.show_runs, { count: runs.length })}
        </CollapsibleTrigger>
        <div className="flex min-w-0 justify-end">
          <RetryRunButton task={last} issueId={issueId} />
        </div>
      </CardFooter>
    </Collapsible>
  );
}
