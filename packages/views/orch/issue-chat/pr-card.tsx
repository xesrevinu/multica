"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  AlertTriangle,
  Check,
  ChevronRight,
  ExternalLink,
  GitMerge,
  Loader2,
  RefreshCw,
  RotateCcw,
  Tags,
  Trash2,
  X,
} from "lucide-react";
import {
  orchPRCardOptions,
  orchPRListOptions,
  useOrchPROp,
  type OrchPRCard,
  type OrchPROp,
} from "@multica/core/orchestrator";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";
import { checksTone, prStateTone, PR_STATE_CHIP, shortSha, TONE_DOT } from "../format";
import { DiffStat, PRStateIcon } from "../parts/chips";
import { CommitGraph, LaneChip, type GraphRow } from "../parts/commit-graph";
import { PersonAvatar } from "../parts/person-avatar";

type Detail = NonNullable<OrchPRCard["pr"]>;

function PRCardSkeleton() {
  return (
    <div className="overflow-hidden rounded-2xl border border-border bg-card p-4" data-testid="orch-pr-card-loading" aria-busy="true">
      <Skeleton className="h-4 w-2/3" />
      <Skeleton className="mt-2 h-3 w-1/2" />
      <Skeleton className="mt-3 h-16 w-full" />
    </div>
  );
}

// PRCard: the Codex-style pull request card — title + state, a commit graph
// with branch lanes (PR branch, and the base branch where it was merged in),
// conflicts, checks, reviews and the actions the GitHub App may take.
// Renders nothing in local mode or when the issue has no PR.
export function PRCard({ wsId, issueKey, hasPR }: { wsId: string; issueKey: string; hasPR: boolean }) {
  const { t } = useT("orch");
  const { data: linked = [] } = useQuery({ ...orchPRListOptions(wsId, issueKey), enabled: hasPR });
  const [number, setNumber] = useState<number | undefined>(undefined);
  const { data, isLoading, isError, refetch, isFetching } = useQuery(orchPRCardOptions(wsId, issueKey, hasPR, number));
  if (isLoading && hasPR) return <PRCardSkeleton />;
  if (isError && hasPR) {
    return (
      <div className="flex items-center justify-between gap-3 rounded-2xl border border-border bg-card px-4 py-3" data-testid="orch-pr-card-error">
        <p className="text-caption text-muted-foreground">{t(($) => $.pr.error)}</p>
        <Button size="sm" variant="outline" disabled={isFetching} onClick={() => void refetch()}>
          {t(($) => $.pr.retry)}
        </Button>
      </div>
    );
  }
  if (!data || data.mode === "local") return null;
  const others = linked.length > 1 ? linked : [];
  return (
    <div className="flex flex-col gap-1.5">
      {others.length > 0 && (
        <div className="flex flex-wrap items-center gap-1 px-1 text-caption text-muted-foreground" data-testid="orch-pr-list">
          <span className="mr-1">{t(($) => $.pr.other_prs)}</span>
          {others.map((p) => {
            const active = (number ?? data.pr?.number ?? data.summary?.number) === p.number;
            return (
              <button
                key={p.number}
                type="button"
                onClick={() => setNumber(p.primary ? undefined : p.number)}
                className={cn(
                  "inline-flex items-center gap-1 rounded-full border px-2 py-0.5 tabular-nums",
                  active ? "border-foreground text-foreground" : "border-border hover:text-foreground",
                )}
                title={p.title}
              >
                <PRStateIcon state={p.merged_at ? "merged" : p.state} />#{p.number}
              </button>
            );
          })}
        </div>
      )}
      <PRCardBody wsId={wsId} issueKey={issueKey} data={data} number={number} />
    </div>
  );
}

function PRCardBody({ wsId, issueKey, data, number }: { wsId: string; issueKey: string; data: OrchPRCard; number?: number }) {
  const { t } = useT("orch");
  const timeAgo = useTimeAgo();
  const op = useOrchPROp(wsId, issueKey);
  const [showChecks, setShowChecks] = useState(false);
  const [showConflicts, setShowConflicts] = useState(true);
  const pr = data.pr;
  const sum = data.summary;
  if (!pr && !sum) return null;
  const prNumber = pr?.number ?? sum!.number;
  const title = pr?.title ?? sum?.title ?? "";
  const url = pr?.url ?? sum?.url ?? "";
  const state = pr?.merged_at ? "merged" : (pr?.state ?? sum?.state ?? "");
  const tone = prStateTone(state, pr?.draft);
  const open = tone === "open" || tone === "draft";
  const base = pr?.base_ref || "main";
  const head = pr?.head_ref || sum?.branch || "";
  const checks = pr?.checks ?? [];
  const failed = checks.filter((c) => checksTone(c.conclusion ?? c.status) === "bad").length;
  const running = checks.filter((c) => c.status !== "completed").length;
  const passed = checks.filter((c) => checksTone(c.conclusion ?? "") === "ok").length;
  const ms = (pr?.mergeable_state ?? sum?.merge_state ?? "").toLowerCase();
  const conflict = ms === "dirty" || pr?.mergeable === false || (sum?.mergeable ?? "").toUpperCase() === "CONFLICTING";
  const behind = pr?.behind_by ?? 0;
  const pendingOp = op.isPending ? (typeof op.variables === "string" ? op.variables : op.variables?.op) : undefined;
  const run = (o: OrchPROp) =>
    op
      .mutateAsync({ op: o, number, headSha: pr?.head_sha || undefined })
      .then(() => o === "delete-branch" && toast.success(t(($) => $.pr.branch_deleted)))
      .catch((err: unknown) => toast.error(t(($) => $.actions.failed, { error: err instanceof Error ? err.message : String(err) })));
  const stateText =
    tone === "merged" ? t(($) => $.pr.merged) : tone === "closed" ? t(($) => $.pr.closed) : tone === "draft" ? t(($) => $.pr.draft) : t(($) => $.pr.open);

  const rows = graphRows(pr, head, base, pendingOp === "update-branch", t(($) => $.pr.merging_base, { base }), t(($) => $.pr.resolving), timeAgo);
  const canDeleteBranch = data.can.delete_branch && !open && pr && !pr.head_deleted && head && head !== base;

  return (
    <div className="overflow-hidden rounded-2xl border border-border bg-card" data-testid="orch-pr-card">
      <div className="flex items-start gap-2 px-4 pt-3">
        <PRStateIcon state={state} draft={pr?.draft} className="mt-0.5 size-4" />
        <div className="min-w-0 flex-1">
          <a href={url} target="_blank" rel="noreferrer" className="line-clamp-2 text-body font-medium text-foreground hover:underline">
            {title} <span className="font-normal text-muted-foreground">#{prNumber}</span>
          </a>
          <div className="mt-1 flex flex-wrap items-center gap-1.5 text-caption text-muted-foreground">
            <span className={cn("inline-flex items-center rounded-full px-2 py-px text-micro font-medium", PR_STATE_CHIP[tone])} data-testid="orch-pr-state">
              {stateText}
            </span>
            {head && <LaneChip name={head} lane={0} />}
            <span aria-hidden>→</span>
            <LaneChip name={base} lane={1} />
            <DiffStat additions={pr?.additions ?? sum?.additions ?? 0} deletions={pr?.deletions ?? sum?.deletions ?? 0} className="ml-1 text-caption" />
            {(pr?.changed_files ?? sum?.changed_files ?? 0) > 0 && <span>· {t(($) => $.pr.files, { count: pr?.changed_files ?? sum?.changed_files ?? 0 })}</span>}
            {(pr?.labels ?? []).map((l) => (
              <span key={l} className="rounded-full bg-muted px-1.5 text-micro">{l}</span>
            ))}
          </div>
        </div>
        {url && (
          <a href={url} target="_blank" rel="noreferrer" aria-label={t(($) => $.pr.open_on_github)} className="mt-0.5 rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground">
            <ExternalLink className="size-3.5" />
          </a>
        )}
      </div>

      {rows.length > 0 && <CommitGraph rows={rows} className="mt-2 px-3" />}

      {open && conflict && (
        <div className="mx-3 mt-2 rounded-lg border border-red-500/30 bg-red-500/5" data-testid="orch-pr-conflicts">
          <button
            type="button"
            onClick={() => setShowConflicts((v) => !v)}
            className="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-left text-caption text-red-700 dark:text-red-300"
          >
            <AlertTriangle className="size-3.5 shrink-0" />
            <span className="font-medium">{t(($) => $.pr.conflicts)}</span>
            {(pr?.conflicts.length ?? 0) > 0 && <span>· {t(($) => $.pr.conflict_count, { count: pr!.conflicts.length })}</span>}
            {(pr?.conflicts.length ?? 0) > 0 && <ChevronRight className={cn("ml-auto size-3.5 transition-transform", showConflicts && "rotate-90")} />}
          </button>
          {showConflicts && (pr?.conflicts.length ?? 0) > 0 && (
            <ul className="flex flex-col gap-0.5 px-2.5 pb-2 pl-7">
              {pr!.conflicts.map((f) => (
                <li key={f} className="truncate font-mono text-micro text-muted-foreground">{f}</li>
              ))}
            </ul>
          )}
        </div>
      )}
      {open && !conflict && (
        <p className={cn("mx-4 mt-2 text-caption", behind > 0 ? "text-amber-700 dark:text-amber-300" : "text-muted-foreground")}>
          {behind > 0
            ? t(($) => $.pr.behind, { count: behind, base })
            : ms === "clean"
              ? t(($) => $.pr.clean)
              : ms === "blocked"
                ? t(($) => $.pr.blocked)
                : t(($) => $.pr.unknown)}
        </p>
      )}
      {!data.live && <p className="mx-4 mt-2 text-micro text-muted-foreground">{t(($) => $.pr.snapshot_only)}</p>}

      {data.live && (
        <div className="mt-2 border-t border-border">
          <button
            type="button"
            onClick={() => setShowChecks((v) => !v)}
            aria-expanded={showChecks}
            className="flex w-full items-center gap-2 px-4 py-2 text-left text-caption text-muted-foreground hover:text-foreground"
            data-testid="orch-pr-checks"
          >
            {checks.length === 0 ? (
              <span>{t(($) => $.pr.no_checks)}</span>
            ) : failed > 0 ? (
              <span className="inline-flex items-center gap-1 text-red-700 dark:text-red-300"><X className="size-3.5" />{t(($) => $.pr.checks_failed, { count: failed })}</span>
            ) : running > 0 ? (
              <span className="inline-flex items-center gap-1"><Loader2 className="size-3.5 animate-spin text-amber-500" />{t(($) => $.pr.checks_running, { count: running })}</span>
            ) : (
              <span className="inline-flex items-center gap-1"><Check className="size-3.5 text-emerald-600 dark:text-emerald-400" />{t(($) => $.pr.checks_passed, { passed, total: checks.length })}</span>
            )}
            {checks.length > 0 && <ChevronRight className={cn("ml-auto size-3.5 transition-transform", showChecks && "rotate-90")} />}
          </button>
          {pr && pr.reviews.length > 0 && (
            <ReviewFaces reviews={pr.reviews} label={(state) => t(($) => $.pr.review_state[state as "APPROVED"] ?? $.pr.review_state.COMMENTED)} />
          )}
          {showChecks && checks.length > 0 && (
            <ul className="flex flex-col gap-0.5 px-4 pb-2">
              {checks.map((c) => (
                <li key={`${c.app}-${c.name}`} className="flex items-center gap-2 text-caption">
                  <span className={cn("size-1.5 shrink-0 rounded-full", TONE_DOT[checksTone(c.conclusion || (c.status === "completed" ? "neutral" : c.status))])} />
                  {c.url ? (
                    <a href={c.url} target="_blank" rel="noreferrer" className="truncate text-foreground hover:underline">{c.name}</a>
                  ) : (
                    <span className="truncate text-foreground">{c.name}</span>
                  )}
                  <span className="ml-auto shrink-0 text-micro text-muted-foreground">{c.conclusion || c.status}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {data.live && (open || canDeleteBranch || data.can.labels) && (
        <div className="flex flex-wrap items-center justify-end gap-1.5 border-t border-border bg-muted/30 px-3 py-2" data-testid="orch-pr-actions">
          {data.orchestrated && open && <span className="mr-auto text-micro text-muted-foreground">{t(($) => $.pr.engine_merges)}</span>}
          {data.can.labels && !data.orchestrated && (
            <Button size="sm" variant="ghost" disabled={op.isPending} onClick={() => run("sync-labels")} data-testid="orch-pr-sync-labels" title={t(($) => $.pr.sync_labels_tip)}>
              {pendingOp === "sync-labels" ? <Loader2 className="size-3.5 animate-spin" /> : <Tags className="size-3.5" />}
              {t(($) => $.pr.sync_labels)}
            </Button>
          )}
          {canDeleteBranch && (
            <Button
              size="sm"
              variant="ghost"
              disabled={op.isPending}
              onClick={() => {
                if (window.confirm(t(($) => $.pr.delete_branch_confirm, { branch: head }))) void run("delete-branch");
              }}
              data-testid="orch-pr-delete-branch"
            >
              {pendingOp === "delete-branch" ? <Loader2 className="size-3.5 animate-spin" /> : <Trash2 className="size-3.5" />}
              {t(($) => $.pr.delete_branch)}
            </Button>
          )}
          {open && data.can.rerun && failed > 0 && (
            <Button size="sm" variant="outline" disabled={op.isPending} onClick={() => run("rerun-checks")} data-testid="orch-pr-rerun">
              {pendingOp === "rerun-checks" ? <Loader2 className="size-3.5 animate-spin" /> : <RotateCcw className="size-3.5" />}
              {t(($) => $.pr.rerun)}
            </Button>
          )}
          {open && data.can.update_branch && (behind > 0 || conflict) && (
            <Button
              size="sm"
              variant="outline"
              disabled={op.isPending || conflict}
              onClick={() => run("update-branch")}
              data-testid="orch-pr-update-branch"
              title={conflict ? t(($) => $.pr.update_branch_conflict_tip, { base }) : t(($) => $.pr.update_branch_tip, { base })}
            >
              {pendingOp === "update-branch" ? <Loader2 className="size-3.5 animate-spin" /> : <RefreshCw className="size-3.5" />}
              {t(($) => $.pr.update_branch, { base })}
            </Button>
          )}
          {open && data.can.merge && !data.orchestrated && (
            <Button size="sm" disabled={op.isPending || conflict} onClick={() => run("merge")} data-testid="orch-pr-merge">
              <GitMerge className="size-3.5" />
              {t(($) => $.pr.merge)}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}

type ReviewFace = { user: string; avatar?: string; state: string; body: string; submitted_at: string };

// Latest verdict per reviewer. A missing or broken avatar becomes an initial,
// so merged PRs don't show a row of empty circles.
function ReviewFaces({ reviews, label }: { reviews: ReviewFace[]; label: (state: string) => string }) {
  const latest = new Map<string, ReviewFace>();
  for (const r of reviews) {
    if (r.user) latest.set(r.user, r);
  }
  const rows = [...latest.values()];
  if (rows.length === 0) return null;
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 pb-2" data-testid="orch-pr-reviewers">
      {rows.map((r) => (
        <span key={r.user} className="inline-flex min-w-0 max-w-full items-center gap-1.5 text-caption" title={r.body || r.user}>
          <PersonAvatar name={r.user} url={r.avatar} />
          <span className="truncate text-foreground">{r.user}</span>
          <span className="shrink-0 text-muted-foreground">{label(r.state)}</span>
        </span>
      ))}
    </div>
  );
}

// graphRows lays the PR's commits out newest first. The first-parent chain
// is lane 0; a merge commit's second parent is the base branch (lane 1),
// closed by a "base tip" row. While "update branch" runs, a pending merge
// row sits on top.
function graphRows(
  pr: Detail | null,
  head: string,
  base: string,
  merging: boolean,
  mergingText: string,
  resolvingText: string,
  timeAgo: (iso: string) => string,
): GraphRow[] {
  if (!pr || pr.commits.length === 0) return [];
  const commits = [...pr.commits].reverse();
  const shas = new Set(pr.commits.map((c) => c.sha));
  const rows: GraphRow[] = [];
  if (merging) {
    rows.push({
      key: "pending-merge",
      lane: 0,
      merge: true,
      pending: true,
      content: (
        <span className="flex min-w-0 flex-1 items-center gap-2 text-label text-muted-foreground" data-testid="orch-pr-merging">
          <span className="truncate">{mergingText}</span>
          <span className="ml-auto inline-flex shrink-0 items-center gap-1 text-caption text-amber-700 dark:text-amber-300">
            <Loader2 className="size-3 animate-spin" />
            {resolvingText}
          </span>
        </span>
      ),
    });
  }
  let baseTip: string | undefined;
  commits.forEach((c, i) => {
    const merge = c.parents.length > 1;
    if (merge && !baseTip) baseTip = c.parents.find((p) => !shas.has(p));
    const msg = c.message.split("\n")[0] ?? "";
    rows.push({
      key: c.sha,
      lane: 0,
      merge,
      content: (
        <>
          <span className={cn("min-w-0 truncate text-label", i === 0 ? "font-medium text-foreground" : "text-foreground")} title={c.message}>
            {msg}
          </span>
          {i === 0 && head && <LaneChip name={head} lane={0} />}
          <span className="ml-auto flex shrink-0 items-center gap-1.5 text-caption text-muted-foreground">
            <span className="font-mono text-micro">{shortSha(c.sha)}</span>
            <span aria-hidden>·</span>
            {c.author ? <PersonAvatar name={c.author} url={c.avatar} className="size-4 text-[9px]" /> : null}
            <span className="max-w-28 truncate">{c.author}</span>
            {c.date && (
              <>
                <span aria-hidden>·</span>
                <span className="tabular-nums">{timeAgo(c.date)}</span>
              </>
            )}
          </span>
        </>
      ),
    });
  });
  if (baseTip || merging) {
    rows.push({
      key: `base-${baseTip ?? "tip"}`,
      lane: 1,
      content: (
        <>
          <LaneChip name={base} lane={1} />
          <span className="ml-auto font-mono text-micro text-muted-foreground">{baseTip ? shortSha(baseTip) : ""}</span>
        </>
      ),
    });
  }
  return rows;
}
