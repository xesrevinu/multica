"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronRight, ExternalLink, GitMerge, GitPullRequest, RefreshCw, RotateCcw } from "lucide-react";
import { orchPRCardOptions, useOrchPROp, type OrchPROp } from "@multica/core/orchestrator";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { checksTone, shortSha, TONE_DOT } from "../format";

// PRCard: live PR state through the GitHub App — checks, commits,
// mergeability, and the actions the installation's permissions allow.
// Renders nothing in local mode or when the issue has no PR.
export function PRCard({ wsId, issueKey, hasPR }: { wsId: string; issueKey: string; hasPR: boolean }) {
  const { t } = useT("orch");
  const { data } = useQuery(orchPRCardOptions(wsId, issueKey, hasPR));
  const op = useOrchPROp(wsId, issueKey);
  const [showCommits, setShowCommits] = useState(false);
  if (!data || data.mode === "local") return null;
  const pr = data.pr;
  const sum = data.summary;
  if (!pr && !sum) return null;
  const number = pr?.number ?? sum!.number;
  const title = pr?.title ?? sum?.title ?? "";
  const url = pr?.url ?? sum?.url ?? "";
  const state = pr?.state ?? sum?.state ?? "";
  const open = state === "open";
  const base = pr?.base_ref || "main";
  const checks = pr?.checks ?? [];
  const failed = checks.filter((c) => checksTone(c.conclusion ?? c.status) === "bad").length;
  const ms = (pr?.mergeable_state ?? sum?.merge_state ?? "").toLowerCase();
  const conflict = ms === "dirty" || pr?.mergeable === false || (sum?.mergeable ?? "").toUpperCase() === "CONFLICTING";
  const behind = pr?.behind_by ?? 0;
  const run = (o: OrchPROp) =>
    op.mutateAsync(o).catch((err: unknown) => toast.error(t(($) => $.actions.failed, { error: err instanceof Error ? err.message : String(err) })));
  const stateChip =
    state === "merged" ? t(($) => $.pr.merged) : state === "closed" ? t(($) => $.pr.closed) : pr?.draft ? t(($) => $.pr.draft) : t(($) => $.pr.open);
  return (
    <div className="rounded-xl border border-border/70 bg-card p-3" data-testid="orch-pr-card">
      <div className="flex items-center gap-2">
        <GitPullRequest className={cn("size-4 shrink-0", state === "merged" ? "text-violet-500" : open ? "text-emerald-500" : "text-muted-foreground")} />
        <a href={url} target="_blank" rel="noreferrer" className="min-w-0 truncate text-body font-medium hover:underline">
          #{number} {title}
        </a>
        <span className="shrink-0 rounded-sm bg-muted px-1.5 text-micro text-muted-foreground">{stateChip}</span>
        <span className="ml-auto shrink-0 font-mono text-micro">
          <span className="text-emerald-600">+{pr?.additions ?? sum?.additions ?? 0}</span>{" "}
          <span className="text-red-600">−{pr?.deletions ?? sum?.deletions ?? 0}</span>
        </span>
        {url && (
          <a href={url} target="_blank" rel="noreferrer" aria-label={t(($) => $.pr.open_on_github)} className="text-muted-foreground hover:text-foreground">
            <ExternalLink className="size-3.5" />
          </a>
        )}
      </div>
      {pr?.head_ref && (
        <p className="mt-1 truncate font-mono text-micro text-muted-foreground">
          {pr.head_ref} → {base} · {shortSha(pr.head_sha)}
        </p>
      )}
      {open && (
        <p className={cn("mt-2 text-caption", conflict ? "text-red-600" : behind > 0 ? "text-amber-600" : "text-muted-foreground")}>
          {conflict
            ? t(($) => $.pr.conflicts)
            : behind > 0
              ? t(($) => $.pr.behind, { count: behind, base })
              : ms === "clean"
                ? t(($) => $.pr.clean)
                : ms === "blocked"
                  ? t(($) => $.pr.blocked)
                  : t(($) => $.pr.unknown)}
        </p>
      )}
      {!data.live && <p className="mt-1 text-micro text-muted-foreground">{t(($) => $.pr.snapshot_only)}</p>}
      {data.live && (
        <div className="mt-2">
          <p className="mb-1 text-micro font-medium uppercase tracking-wide text-muted-foreground">{t(($) => $.pr.checks)}</p>
          {checks.length === 0 ? (
            <p className="text-caption text-muted-foreground">{t(($) => $.pr.no_checks)}</p>
          ) : (
            <ul className="flex flex-col gap-0.5">
              {checks.map((c) => (
                <li key={`${c.app}-${c.name}`} className="flex items-center gap-2 text-caption">
                  <span className={cn("size-1.5 shrink-0 rounded-full", TONE_DOT[checksTone(c.conclusion || (c.status === "completed" ? "neutral" : c.status))])} />
                  {c.url ? (
                    <a href={c.url} target="_blank" rel="noreferrer" className="truncate hover:underline">
                      {c.name}
                    </a>
                  ) : (
                    <span className="truncate">{c.name}</span>
                  )}
                  <span className="ml-auto shrink-0 text-micro text-muted-foreground">{c.conclusion || c.status}</span>
                </li>
              ))}
            </ul>
          )}
          {(pr?.commits.length ?? 0) > 0 && (
            <>
              <button
                type="button"
                onClick={() => setShowCommits((v) => !v)}
                aria-expanded={showCommits}
                className="mt-2 flex items-center gap-1 text-micro font-medium uppercase tracking-wide text-muted-foreground hover:text-foreground"
              >
                <ChevronRight className={cn("size-3 transition-transform", showCommits && "rotate-90")} />
                {t(($) => $.pr.commits)} · {pr!.commits.length}
              </button>
              {showCommits && (
                <ul className="mt-1 flex flex-col gap-0.5">
                  {pr!.commits.map((c) => (
                    <li key={c.sha} className="flex items-baseline gap-2 text-caption">
                      <span className="shrink-0 font-mono text-micro text-muted-foreground">{shortSha(c.sha)}</span>
                      <span className="truncate">{c.message}</span>
                      <span className="ml-auto shrink-0 text-micro text-muted-foreground">{c.author}</span>
                    </li>
                  ))}
                </ul>
              )}
            </>
          )}
        </div>
      )}
      {open && data.live && (data.can.update_branch || data.can.rerun || data.can.merge || data.orchestrated) && (
        <div className="mt-3 flex flex-wrap items-center justify-end gap-1.5">
          {data.can.rerun && failed > 0 && (
            <Button size="sm" variant="outline" disabled={op.isPending} onClick={() => run("rerun-checks")}>
              <RotateCcw className="size-3.5" />
              {t(($) => $.pr.rerun)}
            </Button>
          )}
          {data.can.update_branch && (behind > 0 || conflict) && (
            <Button size="sm" variant="outline" disabled={op.isPending} onClick={() => run("update-branch")}>
              <RefreshCw className="size-3.5" />
              {t(($) => $.pr.update_branch, { base })}
            </Button>
          )}
          {data.can.merge && !data.orchestrated && (
            <Button size="sm" disabled={op.isPending || conflict} onClick={() => run("merge")}>
              <GitMerge className="size-3.5" />
              {t(($) => $.pr.merge)}
            </Button>
          )}
          {data.orchestrated && <span className="text-micro text-muted-foreground">{t(($) => $.pr.engine_merges)}</span>}
        </div>
      )}
    </div>
  );
}
